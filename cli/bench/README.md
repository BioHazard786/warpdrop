# CLI ↔ CLI throughput benchmark

Compares the v2 CLI (single data channel, raw frames) against the old multi-channel CLI (msgpack, N channels),
the gate described in the v2 plan: *if v2 is more than 10% slower on any case, add negotiated lanes.*

## Running it

```sh
export BENCH_DIR=$PWD/_work && mkdir -p $BENCH_DIR   # holds binaries and data
# binaries: new CLI + backend from v2; old CLI + backend built from main's cli/ and backend/
cp <v2 cli binary> $BENCH_DIR/warpdrop;  cp <old cli binary> $BENCH_DIR/warpdrop-old
# data: bench/big/one.bin (1 GiB), bench/small/f1..f2000.bin (512 KiB each)
# servers: new backend on :18082; old backend on :8080 behind `tlsproxy` on :8443
#   (the old CLI hardcodes wss://, so tlsproxy is a throwaway TLS front; it writes cert.pem to its cwd;
#    run the old CLI with SSL_CERT_FILE=<that cert.pem>)
./bench.sh new big 3     # tool: old|new   set: big|mid|small   runs: N
```

`ptyrun.py` runs the old CLI under a pseudo-terminal (its UI needs one) and answers its `[Y/n]` prompt.
The new server rate-limits room creation, so `bench.sh` sleeps 7 s between new-CLI runs.

## Results (loopback, 16 cores, one machine)

| Workload | v2 | old multi-channel |
|---|---|---|
| 1 GiB single file (3 runs) | 72.1 / 78.2 / 72.6 MiB/s | 58.6 / 54.6 / 60.2 MiB/s |
| 200 files, 100 MB | 53.4 MiB/s | 9.5 MiB/s |
| 2 000 files, 1 GB | 41.1 / 61.3 MiB/s | did not finish: ~447 MB after 10 min (~0.7 MiB/s, both ends CPU-bound) |

v2 is faster in every measured case, so **no lanes were added**.

## Not measured

- The plan also called for 80 ms RTT and 0.5% loss via `tc netem`. That needs root, which was unavailable, so only
  loopback was run. High-latency behaviour (the 32 MiB window vs. several parallel channels) is **unverified**.
- No LAN run.
- The old CLI's small-file slowness is probably its per-file progress UI rather than its protocol, so treat the
  small-file ratios as "old CLI as shipped", not "multi-channel in principle".
