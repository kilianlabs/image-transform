#!/usr/bin/env python3
"""Compare admission settings with synthetic images in disposable Docker containers.

Requires Python with Pillow, curl, Docker, and image-transform:concurrency-bench.
See docs/benchmarks/concurrency.md.
"""

import concurrent.futures as futures
import json
import pathlib
import random
import statistics
import subprocess
import tempfile
import threading
import time
import urllib.request

from PIL import Image, ImageDraw

ROOT = pathlib.Path(__file__).resolve().parents[1]
OUT = pathlib.Path(tempfile.mkdtemp(prefix='image-concurrency-benchmark-'))
print(f'Results: {OUT}', flush=True)

def command(*args):
    return subprocess.check_output(args, text=True).strip()

# Deterministic synthetic high-detail inputs; tiny native fixtures exercise codecs.
photo = Image.frombytes('RGB', (2400, 1600), random.Random(42).randbytes(2400 * 1600 * 3))
photo.save(OUT / 'detail.jpg', quality=90)
photo.resize((1920, 1280)).save(OUT / 'detail.png')
Image.linear_gradient('L').resize((10000, 5000)).convert('RGB').save(OUT / 'limit.jpg', quality=90)
frames = []
for i in range(12):
    frame = Image.new('RGB', (640, 480), (i * 17, 90, 160))
    ImageDraw.Draw(frame).ellipse((i * 30, 40, i * 30 + 180, 220), fill=(240, 160, 50))
    frames.append(frame)
frames[0].save(OUT / 'motion.gif', save_all=True, append_images=frames[1:], duration=80, loop=0)
inputs = {
    'jpeg': OUT / 'detail.jpg',
    'png': OUT / 'detail.png',
    'limit': OUT / 'limit.jpg',
    'gif': OUT / 'motion.gif',
    'heif': ROOT / 'internal/native/testdata/sample.heif',
    'avif': ROOT / 'internal/native/testdata/sample.avif',
}

def percentile(values, p):
    return sorted(values)[max(0, int(len(values) * p + 0.999) - 1)] if values else 0

results = []
for name, capacity, vips in [('shared-1-vips2', 1, 2), ('split-2-vips1', 2, 1), ('split-2-vips2', 2, 2)]:
    container = OUT.name + '-' + name
    args = ['docker', 'run', '-d', '--name', container, '--read-only', '--cpus=2', '--memory=2g', '--pids-limit=128',
            '--tmpfs', '/tmp:rw,noexec,nosuid,size=2147483648', '-p', '127.0.0.1::3000',
            '-e', f'MAX_CONCURRENT={capacity}', '-e', 'MAX_INSPECT_CONCURRENT=1', '-e', 'MAX_TRANSFORM_CONCURRENT=1',
            '-e', f'VIPS_THREADS={vips}', 'image-transform:concurrency-bench']
    command(*args)
    base = 'http://' + command('docker', 'port', container, '3000/tcp')
    attempts = []
    jobs = []
    lock = threading.Lock()
    try:
        for _ in range(100):
            try:
                urllib.request.urlopen(base + '/health', timeout=1).close()
                break
            except Exception:
                time.sleep(0.1)
        else:
            raise RuntimeError('service did not start')

        def request(kind, source, query=''):
            job_start = time.monotonic()
            while True:
                attempt_start = time.monotonic()
                reason = ''
                output = OUT / ('response-' + str(threading.get_ident()))
                response = subprocess.run(['curl', '--silent', '--show-error', '--max-time', '135',
                    '--header', 'Content-Type: application/octet-stream', '--header', 'Expect: 100-continue',
                    '--expect100-timeout', '6', '--data-binary', '@' + str(inputs[source]),
                    '--output', str(output), '--write-out', '%{http_code}', base + '/api/' + kind + query], capture_output=True, text=True)
                status = int(response.stdout or '0')
                if response.returncode:
                    reason = response.stderr
                elif status != 200:
                    reason = json.loads(output.read_text()).get('type', '')
                with lock:
                    attempts.append({'kind':kind, 'source':source, 'status':status, 'reason':reason, 'seconds':time.monotonic()-attempt_start})
                if status != 503 or reason != 'urn:image-transform:error:busy' or time.monotonic()-job_start > 180:
                    break
                # Retry quickly to expose contention; applications should use backoff and jitter.
                time.sleep(0.1)
            with lock:
                jobs.append({'kind':kind, 'source':source, 'status':status, 'seconds':time.monotonic()-job_start})

        start = time.monotonic()
        transforms = [('jpeg', '?format=jxl'), ('png', '?format=webp'), ('gif', '?format=jxl'),
                      ('jpeg', '?format=jxl'), ('png', '?format=webp'), ('gif', '?format=webp'),
                      ('avif', '?format=jxl'), ('heif', '?format=webp'), ('limit', '?format=jxl')]
        with futures.ThreadPoolExecutor(max_workers=12) as pool:
            tasks = [pool.submit(request, 'transform', *item) for item in transforms]
            time.sleep(0.15)
            # Three clients making sequential requests, six input types each.
            def inspect_batch():
                for source in ['jpeg', 'png', 'heif', 'avif', 'gif', 'limit']:
                    request('inspect', source)
            tasks += [pool.submit(inspect_batch) for _ in range(3)]
            for task in tasks:
                task.result()
        elapsed = time.monotonic()-start
        peak = int(command('docker', 'exec', container, 'cat', '/sys/fs/cgroup/memory.peak'))
        memory_events = command('docker', 'exec', container, 'cat', '/sys/fs/cgroup/memory.events')
        logs = command('docker', 'logs', container)
        inspection = [job['seconds'] for job in jobs if job['kind'] == 'inspect' and job['status'] == 200]
        result = {'configuration':name, 'elapsed_seconds':elapsed, 'inspection_p50_seconds':statistics.median(inspection),
                  'inspection_p95_seconds':percentile(inspection, .95), 'inspection_max_seconds':max(inspection),
                  'transform_per_minute':60 * len([job for job in jobs if job['kind']=='transform' and job['status']==200])/elapsed,
                  'completed':len([job for job in jobs if job['status']==200]), 'job_count':len(jobs),
                  'busy':sum(a['reason']=='urn:image-transform:error:busy' for a in attempts),
                  'request_timeouts':sum(a['status']==504 for a in attempts), 'transport_errors':sum(a['status']==0 for a in attempts),
                  'peak_memory_mib':peak/2**20, 'memory_events':memory_events, 'jobs':jobs, 'attempts':attempts}
        results.append(result)
        (OUT / 'results.json').write_text(json.dumps(results, indent=2))
        (OUT / (name+'.log')).write_text(logs)
        print(json.dumps({k:v for k,v in result.items() if k not in ('jobs','attempts')}), flush=True)
    finally:
        command('docker', 'rm', '-f', container)
