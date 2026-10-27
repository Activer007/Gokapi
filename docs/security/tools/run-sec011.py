"""SEC-011: real Docker/Compose checks using fresh labelled volumes only.

Python 3 stdlib; DOCKER may name an absolute Docker CLI. Never log credentials,
response bodies, authenticated container logs or capability URLs. Only sanitized
pre-setup startup errors from fresh volumes are retained. No existing volume is used.
"""
import argparse
import base64
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import re
import secrets
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

REPO = Path(__file__).resolve().parents[3]
DOCKER = os.environ.get("DOCKER", "docker")
parser = argparse.ArgumentParser()
parser.add_argument("--red", action="store_true", help="check the inherited Compose contract")
parser.add_argument("--image", help="reuse a locally built image; record its immutable ID")
args = parser.parse_args()
out = Path(tempfile.mkdtemp(prefix="sec011-", dir=REPO / "docs/security/evidence"))
run_id = "sec011-" + uuid.uuid4().hex[:12]
env = {k: v for k, v in os.environ.items() if not k.startswith(("GOKAPI_", "COMPOSE_")) and k != "DOCKER_NONROOT"}
volumes = []
container = None
compose = None


def cmd(*argv, check=True, **kw):
    r = subprocess.run(argv, env=env, cwd=REPO, capture_output=True, timeout=180, **kw)
    if check and r.returncode:
        raise RuntimeError("command failed: " + argv[0] + " " + argv[1] + " (output withheld)")
    return r


def docker(*argv, **kw):
    return cmd(DOCKER, *argv, **kw)


def record(name, value="PASS"):
    with (out / "results.tsv").open("a", encoding="utf-8") as f:
        f.write(name + "\t" + value + "\n")
    print(name + "\t" + value, flush=True)


def require(condition, label):
    if not condition:
        raise AssertionError(label)
    record(label)


def request(path, data=None, headers=None, client=None):
    req = urllib.request.Request(base + path, data=data, headers=headers or {})
    try:
        with (client or browser).open(req, timeout=15) as r:
            return r.status, r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


def ready(path, marker, client=None):
    for _ in range(60):
        try:
            status, body = request(path, client=client)
            if status == 200 and marker in body:
                return
        except (OSError, urllib.error.URLError):
            pass
        time.sleep(1)
    raise AssertionError("HTTP readiness deadline")


try:
    head = cmd("git", "rev-parse", "HEAD").stdout.decode().strip()
    (out / "head.txt").write_text(head + "\n")
    (out / "runner.sha256").write_text(hashlib.sha256(Path(__file__).read_bytes()).hexdigest() + "\n")
    (out / "environment.json").write_bytes(docker("version", "--format", "{{json .}}").stdout)
    with tempfile.TemporaryDirectory(prefix="gokapi-sec011-") as temp:
        temp = Path(temp)
        empty_env = temp / ".env"
        empty_env.write_text("")
        profile = REPO / ("docker-compose.yaml" if args.red else "docker-compose.nonroot.yaml")
        profile_copy = temp / profile.name
        profile_copy.write_bytes(profile.read_bytes())
        (out / "compose.sha256").write_text(hashlib.sha256(profile.read_bytes()).hexdigest() + "\n")
        image = args.image or "gokapi-sec011:" + head[:12]
        with socket.socket() as reservation:
            reservation.bind(("127.0.0.1", 0))
            host_port = reservation.getsockname()[1]
        env.update(GOKAPI_IMAGE=image, GOKAPI_UID="10001", GOKAPI_GID="10001", GOKAPI_PORT=str(host_port),
                   GOKAPI_DATA_VOLUME=run_id + "-data", GOKAPI_CONFIG_VOLUME=run_id + "-config")
        compose = ["compose", "--env-file", str(empty_env), "-p", run_id, "-f", str(profile_copy)]
        config = json.loads(docker(*compose, "config", "--format", "json").stdout)
        service = config["services"]["gokapi"]
        require(service.get("user") == "10001:10001", "compose-explicit-nonroot")
        require(service["ports"][0]["host_ip"] == "127.0.0.1", "compose-loopback-publication")
        require(service["environment"].get("DOCKER_NONROOT") == "false", "compose-no-legacy-chown")
        if not args.image:
            archive = temp / "source.tar"
            with archive.open("wb") as f:
                subprocess.run(["git", "-c", "core.autocrlf=false", "archive", "HEAD"], cwd=REPO, stdout=f, check=True)
            (out / "archive.sha256").write_text(hashlib.sha256(archive.read_bytes()).hexdigest() + "\n")
            with archive.open("rb") as source, (out / "build.log").open("wb") as log:
                built = subprocess.run([DOCKER, "build", "--progress=plain", "-t", image, "-"],
                                       stdin=source, stdout=log, stderr=subprocess.STDOUT, env=env, timeout=1200)
            require(built.returncode == 0, "archive-image-build")
        image_info = json.loads(docker("image", "inspect", image).stdout)[0]
        image_user = image_info["Config"].get("User", "")
        (out / "image.json").write_text(json.dumps({"Id": image_info["Id"], "User": image_user,
                                                   "WorkspaceHead": head, "BuiltFromArchive": not args.image}))
        record("image-default-user", image_user or "root (unchanged)")
        for key in ("GOKAPI_DATA_VOLUME", "GOKAPI_CONFIG_VOLUME"):
            name = env[key]
            require(docker("volume", "inspect", name, check=False).returncode != 0, "fresh-" + key)
            docker("volume", "create", "--label", "gokapi.sec011=" + run_id, name)
            volumes.append(name)
        docker(*compose, "up", "-d", "--pull", "never")
        container = docker(*compose, "ps", "-aq", "gokapi").stdout.decode().strip()
        require(bool(container), "container-created")
        # Fresh root-owned volumes must not silently be chmod/chowned by the app.
        time.sleep(4)
        docker(*compose, "stop")
        info = json.loads(docker("inspect", container).stdout)[0]
        logs = docker("logs", container).stdout + docker("logs", container).stderr
        # This is before any setup payload, login or file exists in these fresh volumes.
        # The application prints proof paths, never proof contents (SEC-010).
        diagnostic = re.sub(r"https?://\S+", "[URL omitted]", logs.decode(errors="replace"))
        (out / "negative-startup.log").write_text(diagnostic, encoding="utf-8")
        (out / "negative-state.json").write_text(json.dumps(info["State"]))
        require(b"permission denied" in logs.lower(), "wrong-owner-startup-rejected")
        mounts = ["-v", volumes[0] + ":/fixture/data", "-v", volumes[1] + ":/fixture/config"]
        owner = docker("run", "--rm", "--user", "0:0", *mounts, "--entrypoint", "stat", image,
                       "-c", "%u:%g", "/fixture/data", "/fixture/config").stdout.decode().splitlines()
        require(owner == ["0:0", "0:0"], "wrong-owner-not-mutated")
        # Change only the two empty test-volume directory entries, never recurse.
        docker("run", "--rm", "--user", "0:0", *mounts, "--entrypoint", "sh", image, "-ec",
               'test -z "$(ls -A /fixture/data)"; test -z "$(ls -A /fixture/config)"; '
               'chown 10001:10001 /fixture/data /fixture/config; chmod 700 /fixture/data /fixture/config')
        docker(*compose, "start")
        address = docker(*compose, "port", "gokapi", "53842").stdout.decode().strip()
        require(address.startswith("127.0.0.1:"), "runtime-loopback-publication")
        base = "http://" + address
        jar = http.cookiejar.CookieJar()
        browser = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(jar))
        anonymous = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        ready("/", b"maintenance")
        require(docker("exec", container, "id", "-u").stdout.strip() == b"10001", "runtime-uid")
        require(docker("exec", container, "id", "-g").stdout.strip() == b"10001", "runtime-gid")
        process_uids = docker("exec", container, "sh", "-ec",
                              'for p in /proc/[0-9]*; do if [ "$(cat "$p/comm" 2>/dev/null)" = gokapi ]; then '
                              'awk \'/^Uid:/{print $2,$3,$4,$5}\' "$p/status"; fi; done').stdout.decode().splitlines()
        require(process_uids == ["10001 10001 10001 10001"], "application-process-uid")
        runtime = json.loads(docker("inspect", container).stdout)[0]
        require(runtime["HostConfig"]["ReadonlyRootfs"] and runtime["HostConfig"]["CapDrop"] == ["ALL"]
                and "no-new-privileges:true" in runtime["HostConfig"]["SecurityOpt"], "runtime-privilege-restrictions")
        proof = docker("exec", container, "sh", "-ec", "cat /app/config/.setup-credential-*").stdout.decode().strip()
        mode = docker("exec", container, "sh", "-ec", "stat -c %a /app/config/.setup-credential-*").stdout.strip()
        require(mode == b"600", "bootstrap-proof-owner-only")
        password = secrets.token_urlsafe(24)
        values = {"localhost_sel": "0", "ssl_sel": "0", "logip_sel": "0", "port": "53842",
                  "public_name": "SEC011 fixture", "url": base + "/", "url_redirection": base + "/",
                  "showfilename_sel": "0", "authentication_sel": "0", "auth_username": "fixture-admin",
                  "auth_pw": password, "storage_sel": "local", "storage_sel_image": "local",
                  "encrypt_sel": "0", "enc_pw": "", "dbtype_sel": "0", "sqlite_location": "/app/config/gokapi.sqlite",
                  "oauth_provider": "", "oauth_id": "", "oauth_secret": "", "oauth_recheck_interval": "12",
                  "oauth_admin_user": "", "oauth_only_registered_users": "false", "oauth_restrict_groups": "false",
                  "oauth_allowed_groups": "", "oauth_scope_groups": "", "auth_header_admin": "",
                  "auth_headerkey": "", "auth_header_only_registered_users": "false",
                  "redis_location": "", "redis_prefix": "", "redis_user": "", "redis_password": "", "redis_ssl_sel": "0"}
        payload = json.dumps([{"name": k, "value": v} for k, v in values.items()]).encode()
        status, _ = request("/setup/setupResult", payload, {"Content-Type": "application/json"})
        require(status == 401, "setup-without-proof-denied")
        require(docker("exec", container, "test", "-e", "/app/config/config.json", check=False).returncode == 1, "unauthorized-no-config")
        auth = base64.b64encode(("setup:" + proof).encode()).decode()
        status, body = request("/setup/setupResult", payload, {"Content-Type": "application/json", "Authorization": "Basic " + auth})
        require(status == 200 and json.loads(body)["result"] == "OK", "authorized-setup")
        ready("/login", b"csrf-token")
        status, body = request("/login")
        csrf = re.search(rb'name="csrf-token" value="([^"]+)"', body).group(1).decode()
        request("/login", urllib.parse.urlencode({"username": "fixture-admin", "password": password, "csrf-token": csrf}).encode(),
                {"Content-Type": "application/x-www-form-urlencoded"})
        require(any(c.name == "session_token" for c in jar), "nonroot-login")
        status, body = request("/auth/token", headers={"permission": "PERM_UPLOAD"})
        require(status == 200, "upload-token")
        token = json.loads(body)["key"]
        content = b"SEC011 synthetic persistent content\n" * 4096
        boundary = "SEC011" + uuid.uuid4().hex
        body = b""
        for k, v in {"allowedDownloads": "10", "expiryDays": "1"}.items():
            body += ("--" + boundary + '\r\nContent-Disposition: form-data; name="' + k + '"\r\n\r\n' + v + "\r\n").encode()
        body += ("--" + boundary + '\r\nContent-Disposition: form-data; name="file"; filename="fixture.bin"\r\nContent-Type: application/octet-stream\r\n\r\n').encode() + content + ("\r\n--" + boundary + "--\r\n").encode()
        status, response = request("/api/files/add", body, {"Content-Type": "multipart/form-data; boundary=" + boundary, "apikey": token})
        require(status == 200, "nonroot-upload")
        file_id = json.loads(response)["FileInfo"]["Id"]
        download_path = "/downloadFile?id=" + urllib.parse.quote(file_id)
        status, downloaded = request(download_path, client=anonymous)
        require(status == 200 and hashlib.sha256(downloaded).digest() == hashlib.sha256(content).digest(), "nonroot-download-sha256")
        docker(*compose, "restart")
        ready("/login", b"csrf-token", client=anonymous)
        status, downloaded = request(download_path, client=anonymous)
        require(status == 200 and downloaded == content, "restart-persistent-download")
        proof_files = docker("exec", container, "sh", "-ec", "find /app/config -name '.setup-credential-*'").stdout.strip()
        require(not proof_files, "existing-config-no-new-bootstrap")
        record("gate", "PASS (local storage / SQLite / real Docker)")
except Exception as exc:
    # Exception messages never include response contents, random credentials or URLs.
    record("failure", type(exc).__name__ + ": " + (str(exc) if isinstance(exc, (AssertionError, RuntimeError)) else "details withheld"))
    raise SystemExit(1)
finally:
    if container:
        docker("rm", "-f", container, check=False)
    for name in volumes:
        info = json.loads(docker("volume", "inspect", name).stdout)[0]
        if info.get("Labels", {}).get("gokapi.sec011") == run_id:
            docker("volume", "rm", name)
    # Compose networks are labelled with this unique project; no existing project is touched.
    networks = docker("network", "ls", "--filter", "label=com.docker.compose.project=" + run_id, "-q", check=False).stdout.decode().split()
    for network in networks:
        docker("network", "rm", network, check=False)
    print("Evidence: " + str(out), flush=True)
