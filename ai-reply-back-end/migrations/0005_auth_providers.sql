-- 0005_auth_providers: поштаға келетін OTP (Resend), Google және Apple арқылы кіру.
--
-- Only additive changes. Existing users keep their ids, plans, quotas, history
-- and sessions; no earlier migration is edited.

-- Пошта расталған сәт. Бұған дейін пошта тек OTP арқылы жазылатын, сондықтан
-- бар жазбалар auth_identities-тегі расталу уақытымен толтырылады.
ALTER TABLE users ADD COLUMN email_verified_at INTEGER;
UPDATE users
SET email_verified_at = (
    SELECT ai.verified_at FROM auth_identities ai
    WHERE ai.kind = 'email' AND ai.value = users.email)
WHERE email IS NOT NULL;

-- Бір қолданушының бірнеше кіру тәсілі: phone | email | google | apple.
-- Google мен Apple үшін value — провайдердің өзгермейтін `sub` идентификаторы;
-- UNIQUE (kind, value) 0001-ден бері бар, яғни бір провайдер тіркелгісі бір ғана
-- қолданушыға тиесілі. Провайдер берген пошта бөлек сақталады.
ALTER TABLE auth_identities ADD COLUMN provider_email TEXT NOT NULL DEFAULT '';
ALTER TABLE auth_identities ADD COLUMN provider_email_verified INTEGER NOT NULL DEFAULT 0;
ALTER TABLE auth_identities ADD COLUMN updated_at INTEGER NOT NULL DEFAULT 0;
UPDATE auth_identities SET updated_at = created_at;
UPDATE auth_identities SET provider_email = value, provider_email_verified = 1 WHERE kind = 'email';

-- OTP: мақсаты (кіру не поштаны тіркелгіге қосу), неге жабылғаны, соңғы өзгеріс.
-- Код бұрынғыдай тек HMAC түрінде сақталады.
ALTER TABLE otp_codes ADD COLUMN purpose TEXT NOT NULL DEFAULT 'login';
ALTER TABLE otp_codes ADD COLUMN consumed_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE otp_codes ADD COLUMN updated_at INTEGER NOT NULL DEFAULT 0;
UPDATE otp_codes SET updated_at = created_at;
CREATE INDEX idx_otp_recent ON otp_codes (identity_kind, identity_value, created_at);
