-- Profile default DNS: a fallback nameserver list a machine's production
-- network inherits when it doesn't specify its own.
ALTER TABLE profiles
    ADD COLUMN default_dns text[] NOT NULL DEFAULT '{}';

-- Machine production network: the friendly "post-install network" for the
-- 2-NIC netboot pattern. When address is set, netbootd renders an NFS-safe
-- late-command that configures the production NIC static (the only default
-- route) and takes the provisioning NIC down. Empty ({}) means "no production
-- override". Shape: {"address":"10.0.0.10/24","gateway":"10.0.0.1","dns":[...]}.
ALTER TABLE machines
    ADD COLUMN install_network jsonb NOT NULL DEFAULT '{}';
