ALTER TABLE profiles
    DROP COLUMN IF EXISTS keyboard_layout,
    DROP COLUMN IF EXISTS keyboard_variant,
    DROP COLUMN IF EXISTS locale,
    DROP COLUMN IF EXISTS timezone;
