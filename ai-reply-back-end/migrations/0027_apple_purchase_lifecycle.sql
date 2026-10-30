-- Retain verified Apple refund/reversal ordering even before a purchase is posted.
-- No purchase FK: unknown transactions and deleted accounts must not lose this state.
CREATE TABLE apple_purchase_lifecycle (
 transaction_id TEXT PRIMARY KEY,
 state TEXT NOT NULL CHECK(state IN ('refunded','revoked','restored')),
 signed_at INTEGER NOT NULL CHECK(signed_at > 0),
 updated_at INTEGER NOT NULL
);
