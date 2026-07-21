-- Per-machine network override: an optional netplan-shaped config that, when
-- set (non-empty), overrides the assigned profile's network for this machine
-- during install. Empty ({}) means "use the profile's network".
ALTER TABLE machines
    ADD COLUMN network_config jsonb NOT NULL DEFAULT '{}';
