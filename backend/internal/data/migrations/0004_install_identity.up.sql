-- Install-user identity: a configurable login username and (optional) password
-- for the installed OS. Both default to empty so existing profiles keep
-- rendering unchanged (empty username -> "ubuntu"; empty password -> the
-- discarded one-time hash, i.e. key-only access).
ALTER TABLE profiles
    ADD COLUMN install_username      text NOT NULL DEFAULT ''
        CHECK (install_username = '' OR install_username ~ '^[a-z_][a-z0-9_-]{0,31}$'),
    ADD COLUMN install_password_hash text NOT NULL DEFAULT '';

-- Relax the "at least one SSH key" rule: a profile is now valid with SSH keys
-- OR a login password (or both), enabling password-only machines.
ALTER TABLE profiles DROP CONSTRAINT IF EXISTS profiles_ssh_authorized_keys_check;
ALTER TABLE profiles ADD CONSTRAINT profiles_access_method_check
    CHECK (cardinality(ssh_authorized_keys) >= 1 OR install_password_hash <> '');
