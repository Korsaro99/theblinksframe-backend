// Package delivery stores delivery zones and riders (BRIEF §9).
package delivery

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"theblinksframe-backend/internal/models"
)

// Store reads delivery zones and riders.
type Store struct{ db *gorm.DB }

// NewStore returns a Store backed by db.
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// ListActiveZones returns active zones, cheapest first, then by name (ignoring case).
func (s *Store) ListActiveZones(ctx context.Context) ([]models.DeliveryZone, error) {
	var zones []models.DeliveryZone
	err := s.db.WithContext(ctx).Where("is_active").Order("fee_pesewas, lower(name)").Find(&zones).Error
	if err != nil {
		return nil, fmt.Errorf("list active zones: %w", err)
	}
	if zones == nil {
		zones = []models.DeliveryZone{}
	}
	return zones, nil
}

// ListActiveRiders returns active riders by name (ignoring case).
func (s *Store) ListActiveRiders(ctx context.Context) ([]models.Rider, error) {
	var riders []models.Rider
	err := s.db.WithContext(ctx).Where("is_active").Order("lower(name)").Find(&riders).Error
	if err != nil {
		return nil, fmt.Errorf("list active riders: %w", err)
	}
	if riders == nil {
		riders = []models.Rider{}
	}
	return riders, nil
}
