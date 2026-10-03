--liquibase formatted sql

--changeset blinks:0002-brands
--comment: Brands (BRIEF §5). The shop's own label "The Blinks" is a brand with is_own_label = true.
CREATE TABLE brands (
  id           uuid        PRIMARY KEY DEFAULT uuidv7(),
  name         text        NOT NULL CHECK (btrim(name) <> ''),
  slug         text        NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
  logo_url     text,
  sells_for    text        NOT NULL CHECK (sells_for IN ('MEN', 'WOMEN', 'BOTH')),
  is_own_label boolean     NOT NULL DEFAULT false,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX brands_name_lower_key ON brands (lower(name));
CREATE TRIGGER brands_set_updated_at BEFORE UPDATE ON brands
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
--rollback DROP TABLE brands;

--changeset blinks:0002-products
--comment: Products (BRIEF §5, §6). Price bands are derived from price_pesewas and never stored.
CREATE TABLE products (
  id                  uuid        PRIMARY KEY DEFAULT uuidv7(),
  slug                text        NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
  model_name          text        NOT NULL CHECK (btrim(model_name) <> ''),
  brand_id            uuid        NOT NULL REFERENCES brands (id) ON DELETE RESTRICT,
  -- TODO(open): "frames and more" may add categories such as prescription frames (BRIEF §5).
  category            text        NOT NULL CHECK (category IN ('SUNGLASSES', 'ACCESSORY')),
  gender              text        NOT NULL CHECK (gender IN ('MEN', 'WOMEN', 'UNISEX', 'NONE')),
  style               text        NOT NULL CHECK (style IN ('AVIATOR', 'ROUND', 'SQUARE', 'CAT_EYE', 'WRAP', 'CASE_OR_ACCESSORY')),
  price_pesewas       bigint      NOT NULL CHECK (price_pesewas > 0),
  -- Last line of defence: stock can never go negative, whatever the code does.
  stock               integer     NOT NULL DEFAULT 0 CHECK (stock >= 0),
  low_stock_threshold integer     NOT NULL DEFAULT 5 CHECK (low_stock_threshold >= 0),
  description         text        NOT NULL DEFAULT '',
  labels              text[]      NOT NULL DEFAULT '{}'
                                  CHECK (labels <@ ARRAY['NEW', 'POLARIZED', 'BESTSELLER']::text[]),
  sku                 text        UNIQUE,
  is_visible          boolean     NOT NULL DEFAULT true,
  status              text        NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT', 'PUBLISHED')),
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  -- Accessories have no gender and use the accessory style; sunglasses always have both (BRIEF §5).
  CONSTRAINT products_accessory_gender CHECK ((category = 'ACCESSORY') = (gender = 'NONE')),
  CONSTRAINT products_accessory_style  CHECK ((category = 'ACCESSORY') = (style = 'CASE_OR_ACCESSORY'))
);
CREATE INDEX products_brand_id_idx ON products (brand_id);
CREATE INDEX products_storefront_idx ON products (category, gender) WHERE status = 'PUBLISHED' AND is_visible;
CREATE TRIGGER products_set_updated_at BEFORE UPDATE ON products
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
--rollback DROP TABLE products;

--changeset blinks:0002-product-colors
--comment: Frame colours shown as swatches. TODO(open): stock per colour (BRIEF §14 q10).
CREATE TABLE product_colors (
  id         uuid    PRIMARY KEY DEFAULT uuidv7(),
  product_id uuid    NOT NULL REFERENCES products (id) ON DELETE CASCADE,
  name       text    NOT NULL CHECK (btrim(name) <> ''),
  hex        text    NOT NULL CHECK (hex ~ '^#[0-9A-Fa-f]{6}$'),
  position   integer NOT NULL DEFAULT 0
);
CREATE INDEX product_colors_product_id_idx ON product_colors (product_id, position);
--rollback DROP TABLE product_colors;

--changeset blinks:0002-product-media
--comment: Photos and videos. The storage provider is chosen in M2; a storage key column may be added then.
CREATE TABLE product_media (
  id         uuid    PRIMARY KEY DEFAULT uuidv7(),
  product_id uuid    NOT NULL REFERENCES products (id) ON DELETE CASCADE,
  type       text    NOT NULL CHECK (type IN ('PHOTO', 'VIDEO')),
  role       text    NOT NULL CHECK (role IN ('COVER', 'WORN', 'SIDE', 'OTHER')),
  url        text    NOT NULL,
  width      integer NOT NULL CHECK (width > 0),
  height     integer NOT NULL CHECK (height > 0),
  position   integer NOT NULL DEFAULT 0
);
CREATE INDEX product_media_product_id_idx ON product_media (product_id, position);
-- One cover photo per product.
CREATE UNIQUE INDEX product_media_one_cover_key ON product_media (product_id) WHERE role = 'COVER';
--rollback DROP TABLE product_media;
