# TODO

- Multi-tenant kernel isolation: deployed containers share the host kernel beyond resource limits + `cap_drop`/`security_opt` hardening (no gVisor/Kata/seccomp-profile-per-tenant). A kernel-level exploit in one tenant's container can affect others on the same node. Acceptable today since Litepod assumes a trusted control plane choosing what gets deployed, but revisit if the platform starts accepting less-trusted workloads.
