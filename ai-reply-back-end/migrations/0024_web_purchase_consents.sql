-- Separate purchase consent; mobile AI-processing consent is unchanged.
CREATE TABLE billing_web_purchase_consents (
    order_id         TEXT PRIMARY KEY REFERENCES billing_orders(id) ON DELETE CASCADE,
    user_id          TEXT REFERENCES users(id) ON DELETE SET NULL,
    product_code     TEXT NOT NULL,
    catalog_version  INTEGER NOT NULL,
    price_minor      INTEGER NOT NULL,
    units            INTEGER NOT NULL,
    terms_version    TEXT NOT NULL,
    source           TEXT NOT NULL CHECK (source = 'website'),
    accepted_at      INTEGER NOT NULL
);
