--liquibase formatted sql

--changeset blinks:0010-product-media-uploads
--comment: Uploaded photos and videos (POST /admin/products/{id}/media). variants are the resized copies of a photo
-- ([{"width":400,"height":300,"url":"…"}], smallest first) for srcset; videos have none. storage_prefix is
-- where the files live in the media bucket, so deleting the media deletes them; NULL for media added by hand
-- (the seed's sample photos).
ALTER TABLE product_media
  ADD COLUMN variants       jsonb       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(variants) = 'array'),
  ADD COLUMN storage_prefix text        UNIQUE,
  ADD COLUMN content_type   text,
  ADD COLUMN size_bytes     bigint      CHECK (size_bytes > 0),
  ADD COLUMN created_at     timestamptz NOT NULL DEFAULT now();
--rollback ALTER TABLE product_media DROP COLUMN variants, DROP COLUMN storage_prefix, DROP COLUMN content_type, DROP COLUMN size_bytes, DROP COLUMN created_at;
