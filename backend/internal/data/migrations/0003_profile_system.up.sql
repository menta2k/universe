-- System settings for the installed OS: keyboard layout, locale, timezone.
-- All have sensible defaults so existing profiles keep rendering unchanged.
ALTER TABLE profiles
    ADD COLUMN keyboard_layout  text NOT NULL DEFAULT 'us'
        CHECK (keyboard_layout ~ '^[a-z]{2,}$'),
    ADD COLUMN keyboard_variant text NOT NULL DEFAULT '',
    ADD COLUMN locale           text NOT NULL DEFAULT 'en_US.UTF-8',
    ADD COLUMN timezone         text NOT NULL DEFAULT '';
