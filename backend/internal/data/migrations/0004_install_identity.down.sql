-- Restore the mandatory-SSH-key rule and drop the install identity columns.
-- Best-effort: re-adding the key requirement fails if any profile was saved
-- password-only (no keys); such rows must be given a key before rolling back.
ALTER TABLE profiles DROP CONSTRAINT IF EXISTS profiles_access_method_check;
ALTER TABLE profiles ADD CONSTRAINT profiles_ssh_authorized_keys_check
    CHECK (cardinality(ssh_authorized_keys) >= 1);

ALTER TABLE profiles
    DROP COLUMN IF EXISTS install_username,
    DROP COLUMN IF EXISTS install_password_hash;
