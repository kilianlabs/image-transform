# Inspection and conversion concurrency

Measured on 2026-11-05 using the native runtime pinned in the Dockerfile,
on Docker Desktop Linux ARM64. Each container had two CPUs, 2 GiB memory, a 2 GiB
tmpfs, a read-only root, and 128 PIDs. Native codec versions and encoding options
were identical across configurations. Each configuration ran once; these are
synthetic capacity checks, not a production latency guarantee.

## Workload

Nine concurrent conversion submissions competed with three clients each submitting
six inspections sequentially. Inputs were a deterministic high-detail 2400×1600
JPEG, 1920×1280 PNG, twelve-frame 640×480 GIF, 10000×5000 gradient JPEG at the
50-million-pixel limit, and the existing 24×16 HEIF and AVIF fixtures. Conversions
included full-resolution JXL for the detailed JPEG and the 50-megapixel JPEG,
plus WebP, animated JXL/WebP, and HEIF/AVIF input coverage.

The harness used `Expect: 100-continue` and retried explicit busy responses after
100 ms to measure eventual completion under contention. This is a stress-test
interval; applications should choose backoff and jitter for their workload. All
configurations rejected excess requests immediately, without a waiting queue.
The baseline used one shared active slot; the split configurations used an
overall limit of two and one active request per operation. Request timeout was 120 s.

## Results

| Measurement | Shared 1, VIPS 2 | Split 2/1/1, VIPS 1 | Split 2/1/1, VIPS 2 |
| --- | ---: | ---: | ---: |
| Completed jobs | 27/27 | 27/27 | 27/27 |
| Inspection end-to-end p50 | 0.086 s | 0.381 s | 0.105 s |
| Inspection end-to-end p95 / maximum | 23.052 s | 2.183 s | 1.334 s |
| Conversion throughput¹ | 19.9/min | 12.2/min | 21.4/min |
| Total workload duration | 27.19 s | 44.18 s | 25.24 s |
| Busy responses | 1291 | 1188 | 781 |
| Request timeouts / transport errors | 0 / 0 | 0 / 0 | 0 / 0 |
| Peak container memory including tmpfs | 698.4 MiB | 553.3 MiB | 705.5 MiB |
| OOM / OOM kills | 0 / 0 | 0 / 0 | 0 / 0 |

¹ Nine completed conversions divided by total mixed-workload time. End-to-end
inspection latency includes time spent retrying busy responses in the harness.

Memory comes from the container's cgroup `memory.peak`, including tmpfs and startup
probes; `memory.events` supplies OOM counts. Busy counts are individual attempts,
not failed uploads or permanently failed jobs. Eighteen inspection samples are too
few to characterize a production tail; the reported p95 is the slowest sample.
The HEIF/AVIF fixtures exercise codecs, not high-resolution phone-photo performance.
The large gradient is easier to encode than a detailed 50-megapixel photograph.
Backend tests also ran on the host during this sample, so comparisons include
variable host load.

## Interpretation

Separate operation limits reduced inspection delays while conversions were
running. In this sample, `VIPS_THREADS=2` also completed the workload faster than
the one-thread configuration, within the same container memory limit. A single
run does not establish a throughput advantage over shared capacity.

Pending work and retry scheduling belong to the calling application. Re-measure
on your deployment hardware with representative photos and animations before
increasing image limits or concurrency. See
[concurrency configuration](../operations.md#size-resources-for-your-workload).

## Reproduce

From the Image Transform checkout, with Docker, curl, and Python 3 with Pillow:

```sh
docker build --target runtime -t image-transform:concurrency-bench .
python3 scripts/benchmark-concurrency.py
```

The script creates and removes only its own disposable containers. Its printed
temporary directory retains generated inputs, raw request timings, cgroup data,
service logs, and aggregate JSON results. Timing varies with host load and arrival
order; avoid running builds or other resource-heavy work during comparisons.
