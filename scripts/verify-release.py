#!/usr/bin/env python3
"""Verify a downloaded archive and exercise its binaries in an isolated workspace."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import tarfile
import tempfile
import time
import urllib.request


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path, help="archive and checksums.txt directory")
    parser.add_argument("version", help="expected version without the v prefix")
    args = parser.parse_args()
    require(re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?", args.version), "invalid version")
    os.umask(0o077)
    system = platform.system().lower()
    arch = {"x86_64": "amd64", "arm64": "arm64", "aarch64": "arm64"}[platform.machine()]
    target = f"{system}_{arch}"
    archive = args.directory / f"flagctl_{args.version}_{target}.tar.gz"
    entries = [line.split() for line in (args.directory / "checksums.txt").read_text().splitlines()]
    matches = [entry[0] for entry in entries if len(entry) == 2 and entry[1] == archive.name]
    require(len(matches) == 1, "archive must have exactly one checksum entry")
    require(hashlib.sha256(archive.read_bytes()).hexdigest() == matches[0], "archive checksum mismatch")
    print(f"Verified SHA-256: {archive.name}", flush=True)

    # Do not inherit user endpoints, Terraform config/state, or cloud credentials.
    env = {k: os.environ[k] for k in ("PATH", "HOME", "TMPDIR", "SYSTEMROOT") if k in os.environ}
    env.update(TF_IN_AUTOMATION="1", CHECKPOINT_DISABLE="1")
    with tempfile.TemporaryDirectory(prefix="flagctl-release-") as temporary:
        root = Path(temporary)
        binaries = root / "bin"
        binaries.mkdir()
        provider_name = f"terraform-provider-flagctl_v{args.version}"
        expected = {"flagd", "flagctl", provider_name}
        with tarfile.open(archive) as bundle:
            names = [m.name for m in bundle.getmembers()]
            require(len(names) == len(set(names)), "duplicate archive paths")
            for member in bundle.getmembers():
                path = Path(member.name)
                require(not path.is_absolute() and ".." not in path.parts, "unsafe archive path")
                require(member.isfile() or member.isdir(), "unexpected archive link or special file")
            for name in expected:
                member = bundle.getmember(name)
                require(member.isfile() and member.mode & 0o111, f"missing executable: {name}")
                (binaries / name).write_bytes(bundle.extractfile(member).read())
                (binaries / name).chmod(0o700)

        def run(command, cwd=root, codes=(0,)):
            result = subprocess.run([str(c) for c in command], cwd=cwd, env=env,
                                    text=True, capture_output=True, timeout=90)
            require(result.returncode in codes,
                    f"Command failed: {command}\n{result.stdout}\n{result.stderr}")
            return result

        for name in expected:
            output = run([binaries / name, "--version"]).stdout.strip()
            label = "terraform-provider-flagctl" if name == provider_name else name
            require(output == f"{label} version {args.version}", f"wrong embedded version: {output}")
        require(not (root / "data").exists(), "version query created a database")
        print("All three embedded versions match", flush=True)

        process = None
        log_file = None

        def stop():
            nonlocal process, log_file
            if process is not None:
                try:
                    process.terminate()
                    require(process.wait(timeout=10) == 0, "service did not shut down cleanly")
                finally:
                    if process.poll() is None:
                        process.kill()
                        process.wait(timeout=5)
                    process = None
                    log_file.close()

        def start():
            nonlocal process, log_file
            log = root / "service.log"
            log_file = log.open("w")
            process = subprocess.Popen([str(binaries / "flagd"), "--listen", "127.0.0.1:0",
                                        "--data-dir", str(root / "data")],
                                       cwd=root, env=env, stdout=log_file, stderr=log_file)
            for _ in range(100):
                require(process.poll() is None, f"service exited: {log.read_text()}")
                match = re.search(r"address=(127\.0\.0\.1:[0-9]+)", log.read_text())
                if match:
                    env["FLAGCTL_SERVER"] = "http://" + match[1]
                    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
                    with opener.open(env["FLAGCTL_SERVER"] + "/healthz", timeout=5) as response:
                        require(json.load(response) == {"status": "ok"}, "health check failed")
                    return
                time.sleep(0.1)
            raise RuntimeError("service startup timed out")

        def cli(*command, codes=(0,)):
            return run([binaries / "flagctl", "flags", *command], codes=codes)

        def get(key, environment="dev"):
            return json.loads(cli("get", key, "--env", environment, "--output", "json").stdout)

        try:
            start()
            cli("create", "release_smoke", "--env", "dev", "--description", "release check")
            cli("create", "release_smoke", "--env", "prod")
            cli("toggle", "release_smoke", "--env", "dev", "--enabled=true")
            require(get("release_smoke")["enabled"], "toggle failed")
            require(not get("release_smoke", "prod")["enabled"], "environment isolation failed")
            require("release_smoke" in cli("list", "--env", "dev").stdout, "table listing failed")
            stop()
            start()
            require(get("release_smoke")["enabled"], "restart lost the flag")
            for environment in ("dev", "prod"):
                cli("delete", "release_smoke", "--env", environment)
                result = cli("get", "release_smoke", "--env", environment, codes=(1,))
                require("404" in result.stderr, "deleted flag did not return 404")
            print("CLI lifecycle, environment isolation, restart persistence, and deletion passed", flush=True)

            # Install the actual versioned provider via an offline filesystem mirror.
            # This exercises Terraform init/version selection instead of dev_overrides.
            mirror = root / "mirror"
            install = mirror / "registry.terraform.io/ahmedr1zwan/flagctl" / args.version / target
            install.mkdir(parents=True)
            shutil.copy2(binaries / provider_name, install / provider_name)
            config = root / "terraform.tfrc"
            config.write_text('disable_checkpoint = true\nprovider_installation {\n'
                              '  filesystem_mirror {\n    path = ' + json.dumps(str(mirror)) + '\n'
                              '    include = ["registry.terraform.io/ahmedr1zwan/flagctl"]\n  }\n}\n')
            env["TF_CLI_CONFIG_FILE"] = str(config)
            workspace = root / "terraform"
            workspace.mkdir()
            hcl = '''terraform {
  required_providers {
    flagctl = {
      source = "ahmedr1zwan/flagctl"
      version = "VERSION"
    }
  }
}
provider "flagctl" {}
resource "flagctl_flag" "smoke" {
  environment = "dev"
  key = "terraform_release"
  enabled = true
}
'''.replace("VERSION", args.version)
            (workspace / "main.tf").write_text(hcl)

            def terraform(*command, codes=(0,)):
                return run(["terraform", command[0], "-no-color", *command[1:]], cwd=workspace, codes=codes)

            terraform("init", "-input=false")
            terraform("validate")
            terraform("apply", "-input=false", "-auto-approve")
            require(get("terraform_release")["enabled"], "Terraform create failed")
            terraform("plan", "-input=false", "-detailed-exitcode")
            (workspace / "main.tf").write_text(hcl.replace("enabled = true", "enabled = false"))
            terraform("apply", "-input=false", "-auto-approve")
            require(not get("terraform_release")["enabled"], "Terraform update failed")
            cli("toggle", "terraform_release", "--env", "dev", "--enabled=true")
            terraform("plan", "-input=false", "-detailed-exitcode", codes=(2,))
            terraform("apply", "-input=false", "-auto-approve")
            require(not get("terraform_release")["enabled"], "Terraform drift reconciliation failed")
            run(["terraform", "state", "rm", "flagctl_flag.smoke"], cwd=workspace)
            terraform("import", "-input=false", "flagctl_flag.smoke", "dev/terraform_release")
            terraform("plan", "-input=false", "-detailed-exitcode")
            terraform("destroy", "-input=false", "-auto-approve")
            require("404" in cli("get", "terraform_release", "--env", "dev", codes=(1,)).stderr,
                    "Terraform destroy left its flag")
            print("Terraform mirror init, apply, no-op plan, update, drift, import, and destroy passed", flush=True)
        finally:
            stop()
    print(f"Release smoke passed: {target}", flush=True)


if __name__ == "__main__":
    main()
