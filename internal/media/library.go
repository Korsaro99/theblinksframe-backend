// Package media handles product photos and videos: it checks and resizes uploads, keeps the
// files in Storage (Oracle Object Storage in production) and records them in product_media.
//
// Uploads go through the API, not straight from the browser to the bucket, so that every
// file is checked by its content, photos lose their metadata (GPS location included), and
// oversized or malformed files never reach customers.
package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"theblinksframe-backend/internal/catalog"
	"theblinksframe-backend/internal/models"
)

const (
	maxMediaPerProduct = 12
	// waitForTurn is how long an upload waits while another one is being processed.
	waitForTurn = 30 * time.Second
)

// Media types and roles (product_media.type and .role).
const (
	TypePhoto = "PHOTO"
	TypeVideo = "VIDEO"
	RoleCover = "COVER"
	RoleOther = "OTHER"
)

var (
	// ErrNotFound means the product or the media does not exist.
	ErrNotFound = errors.New("media not found")
	// ErrOff means no media storage is configured.
	ErrOff = errors.New("media storage is off")
	// ErrBusy means another upload took too long to finish.
	ErrBusy = errors.New("media processing busy")
	// ErrTooLarge means the file is over its size limit; the message says which.
	ErrTooLarge = errors.New("file too large")
	// ErrMediaLimit means the product already has maxMediaPerProduct media.
	ErrMediaLimit = errors.New("too many media")
	// ErrCoverRequired means the change would leave a published product without a cover.
	ErrCoverRequired = errors.New("published product needs a cover")
	// ErrCoverMustBePhoto means a video was asked to be the cover.
	ErrCoverMustBePhoto = errors.New("cover must be a photo")
)

// tooLarge wraps ErrTooLarge with a message to show.
func tooLarge(msg string) error { return fmt.Errorf("%w: %s", ErrTooLarge, msg) }

// Library records product media and keeps their files.
type Library struct {
	db    *gorm.DB
	store Storage // nil: uploads are off
	log   *slog.Logger
	turn  chan struct{}
}

// NewLibrary returns a Library. store may be nil, which turns uploads off.
func NewLibrary(db *gorm.DB, store Storage, log *slog.Logger) *Library {
	return &Library{db: db, store: store, log: log, turn: make(chan struct{}, 1)}
}

// Upload checks, processes and stores one photo or video and adds it to the product. role is
// COVER, WORN, SIDE or OTHER; a new cover moves the old one to OTHER.
func (l *Library) Upload(ctx context.Context, productID uuid.UUID, role string, body io.Reader) (models.ProductMedia, error) {
	if l.store == nil {
		return models.ProductMedia{}, ErrOff
	}
	if role == "" {
		role = RoleOther
	}
	// Fail fast, before reading a large body, if the product can't take more media.
	if _, n, err := l.productState(l.db.WithContext(ctx), productID, false); err != nil {
		return models.ProductMedia{}, err
	} else if n >= maxMediaPerProduct {
		return models.ProductMedia{}, ErrMediaLimit
	}

	data, err := io.ReadAll(io.LimitReader(body, maxVideoBytes+1))
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig) || len(data) > maxVideoBytes:
		return models.ProductMedia{}, tooLarge("Videos can be up to 50 MB.")
	case err != nil:
		return models.ProductMedia{}, fmt.Errorf("read upload: %w", err)
	}
	isVideo := len(data) >= 12 && string(data[4:8]) == "ftyp"
	if !isVideo && len(data) > maxPhotoBytes {
		return models.ProductMedia{}, tooLarge("Photos can be up to 15 MB.")
	}
	if isVideo && role == RoleCover {
		return models.ProductMedia{}, ErrCoverMustBePhoto
	}

	// One upload is processed at a time: decoding a big photo takes tens of megabytes, and
	// the VM has 1 GB for everything.
	select {
	case l.turn <- struct{}{}:
		defer func() { <-l.turn }()
	case <-time.After(waitForTurn):
		return models.ProductMedia{}, ErrBusy
	case <-ctx.Done():
		return models.ProductMedia{}, ctx.Err()
	}

	m := models.ProductMedia{ID: uuid.Must(uuid.NewV7()), ProductID: productID, Role: role, Variants: models.MediaVariants{}}
	prefix := "products/" + productID.String() + "/" + m.ID.String()
	m.StoragePrefix = &prefix
	size := int64(len(data))
	m.SizeBytes = &size
	if err := l.storeFiles(ctx, &m, prefix, data, isVideo); err != nil {
		l.cleanUp(prefix)
		return models.ProductMedia{}, err
	}

	err = l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Locking the product serialises uploads and cover changes on it; count again under it.
		if _, n, err := l.productState(tx, productID, true); err != nil {
			return err
		} else if n >= maxMediaPerProduct {
			return ErrMediaLimit
		}
		if role == RoleCover {
			if err := tx.Exec(`UPDATE product_media SET role = 'OTHER' WHERE product_id = ? AND role = 'COVER'`, productID).Error; err != nil {
				return fmt.Errorf("move old cover: %w", err)
			}
		}
		if err := tx.Raw(`SELECT COALESCE(max(position) + 1, 0) FROM product_media WHERE product_id = ?`, productID).
			Scan(&m.Position).Error; err != nil {
			return fmt.Errorf("next position: %w", err)
		}
		if err := tx.Exec(`INSERT INTO product_media (id, product_id, type, role, url, width, height, position, variants,
				storage_prefix, content_type, size_bytes) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, m.ProductID, m.Type, m.Role, m.URL, m.Width, m.Height, m.Position, m.Variants,
			m.StoragePrefix, m.ContentType, m.SizeBytes).Error; err != nil {
			return fmt.Errorf("insert media: %w", err)
		}
		return nil
	})
	if err != nil {
		l.cleanUp(prefix)
		return models.ProductMedia{}, err
	}
	return l.get(l.db.WithContext(ctx), productID, m.ID)
}

// storeFiles processes data and writes its files, filling in m's type, size, URL and variants.
func (l *Library) storeFiles(ctx context.Context, m *models.ProductMedia, prefix string, data []byte, isVideo bool) error {
	if isVideo {
		v, err := ProbeVideo(data)
		if err != nil {
			return err
		}
		key := prefix + "/video.mp4"
		if v.ContentType == "video/quicktime" {
			key = prefix + "/video.mov"
		}
		if err := l.store.Put(ctx, key, v.ContentType, data); err != nil {
			return err
		}
		m.Type, m.URL, m.Width, m.Height, m.ContentType = TypeVideo, l.store.URL(key), v.Width, v.Height, &v.ContentType
		return nil
	}

	variants, err := ProcessPhoto(data)
	if err != nil {
		return err
	}
	kind := http.DetectContentType(data)
	if err := l.store.PutOriginal(ctx, prefix+"/original", kind, data); err != nil {
		return err
	}
	for _, v := range variants {
		key := fmt.Sprintf("%s/w%d.jpg", prefix, v.Width)
		if err := l.store.Put(ctx, key, "image/jpeg", v.JPEG); err != nil {
			return err
		}
		m.Variants = append(m.Variants, models.MediaVariant{Width: v.Width, Height: v.Height, URL: l.store.URL(key)})
	}
	top := m.Variants[len(m.Variants)-1]
	m.Type, m.URL, m.Width, m.Height, m.ContentType = TypePhoto, top.URL, top.Width, top.Height, &kind
	return nil
}

// Patch changes a media's role or position. Making it the cover moves the old cover to OTHER.
func (l *Library) Patch(ctx context.Context, productID, mediaID uuid.UUID, role *string, position *int) (models.ProductMedia, error) {
	err := l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		published, _, err := l.productState(tx, productID, true)
		if err != nil {
			return err
		}
		m, err := l.get(tx, productID, mediaID)
		if err != nil {
			return err
		}
		set := map[string]any{}
		if role != nil && *role != m.Role {
			switch {
			case *role == RoleCover && m.Type != TypePhoto:
				return ErrCoverMustBePhoto
			case *role == RoleCover:
				if err := tx.Exec(`UPDATE product_media SET role = 'OTHER' WHERE product_id = ? AND role = 'COVER'`, productID).Error; err != nil {
					return fmt.Errorf("move old cover: %w", err)
				}
			case m.Role == RoleCover && published:
				return ErrCoverRequired
			}
			set["role"] = *role
		}
		if position != nil {
			set["position"] = *position
		}
		if len(set) == 0 {
			return nil
		}
		return tx.Model(&models.ProductMedia{}).Where("id = ?", mediaID).Updates(set).Error
	})
	if err != nil {
		return models.ProductMedia{}, err
	}
	return l.get(l.db.WithContext(ctx), productID, mediaID)
}

// Delete removes a media and then its files. A published product's cover can't be deleted.
func (l *Library) Delete(ctx context.Context, productID, mediaID uuid.UUID) error {
	var prefix *string
	err := l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		published, _, err := l.productState(tx, productID, true)
		if err != nil {
			return err
		}
		m, err := l.get(tx, productID, mediaID)
		if err != nil {
			return err
		}
		if m.Role == RoleCover && published {
			return ErrCoverRequired
		}
		prefix = m.StoragePrefix
		return tx.Exec(`DELETE FROM product_media WHERE id = ?`, mediaID).Error
	})
	if err != nil {
		return err
	}
	if prefix != nil {
		l.cleanUp(*prefix)
	}
	return nil
}

// ProductFiles returns the storage prefixes of a product's media, so the files can be
// deleted with DeleteFiles after the product itself is deleted.
func (l *Library) ProductFiles(ctx context.Context, productID uuid.UUID) ([]string, error) {
	var prefixes []string
	err := l.db.WithContext(ctx).Raw(`SELECT storage_prefix FROM product_media
		WHERE product_id = ? AND storage_prefix IS NOT NULL`, productID).Scan(&prefixes).Error
	return prefixes, err
}

// DeleteFiles deletes stored files by prefix. Failures are logged, not returned: the database
// row is already gone, and a leftover file costs only storage.
func (l *Library) DeleteFiles(prefixes []string) {
	for _, p := range prefixes {
		l.cleanUp(p)
	}
}

func (l *Library) cleanUp(prefix string) {
	if l.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := l.store.DeletePrefix(ctx, prefix); err != nil {
		l.log.Error("media files left behind; delete them from the bucket", slog.String("prefix", prefix), slog.Any("error", err))
	}
}

// productState checks the product exists; with lock it also locks the product row until the
// transaction ends. It reports whether the product is published and how many media it has.
func (l *Library) productState(db *gorm.DB, productID uuid.UUID, lock bool) (published bool, media int, err error) {
	q := `SELECT status, (SELECT count(*) FROM product_media WHERE product_id = p.id) AS media
		FROM products p WHERE p.id = ?`
	if lock {
		q += ` FOR UPDATE`
	}
	var rows []struct {
		Status string
		Media  int
	}
	if err := db.Raw(q, productID).Scan(&rows).Error; err != nil {
		return false, 0, fmt.Errorf("product: %w", err)
	}
	if len(rows) == 0 {
		return false, 0, ErrNotFound
	}
	return rows[0].Status == catalog.StatusPublished, rows[0].Media, nil
}

func (l *Library) get(db *gorm.DB, productID, mediaID uuid.UUID) (models.ProductMedia, error) {
	var rows []models.ProductMedia
	if err := db.Where("id = ? AND product_id = ?", mediaID, productID).Limit(1).Find(&rows).Error; err != nil {
		return models.ProductMedia{}, fmt.Errorf("media: %w", err)
	}
	if len(rows) == 0 {
		return models.ProductMedia{}, ErrNotFound
	}
	return rows[0], nil
}
