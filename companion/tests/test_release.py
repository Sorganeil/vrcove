"""Exercise publishing with a local fixture repository and a fake gh; no network."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest
import zipfile


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.source = Path(__file__).resolve().parents[1]
        self.repo = self.root / "repo"
        self.app = self.repo / "companion"
        self.app.mkdir(parents=True)
        for name in ["release.sh", "package-release.py", "README.md"]:
            shutil.copyfile(self.source / name, self.app / name)
        (self.repo / "LICENSE").write_text("test license")
        (self.repo / ".gitignore").write_text("companion/dist/\n")
        (self.app / "build.sh").write_text('''#!/usr/bin/env bash
set -eu
mkdir -p dist
case "$1" in
  linux)
    printf '#!/usr/bin/env bash\nprintf "VRCove Desktop %%s (fixture)\\n" "$VERSION"\n' > dist/vrcove-desktop
    chmod +x dist/vrcove-desktop
    ;;
  windows)
    printf 'MZfixture-desktop' > dist/vrcove-desktop.exe
    ;;
esac
''')
        subprocess.run(["git", "init", "-q", str(self.repo)], check=True)
        self.git("add", ".")
        self.git("-c", "user.name=Release Test", "-c", "user.email=test@example.test", "commit", "-qm", "fixture")
        self.git("tag", "v1.2.3")
        self.git("tag", "v1.2.3-rc.1")
        bindir = self.root / "bin"
        bindir.mkdir()
        (bindir / "go").write_text("#!/bin/sh\nexit 0\n")
        (bindir / "gh").write_text('''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
a=sys.argv[1:]
root=Path(os.environ['MOCK_DIR'])
with (root/'calls').open('a') as log: log.write(json.dumps(a)+'\\n')
state=root/'draft'
if a[0]=='api':
 print('wrong' if os.environ.get('MOCK_WRONG_TAG') else os.environ['COMMIT'])
elif a[:2]==['release','view']:
 if not state.exists(): sys.exit(1)
 if 'isDraft' in a: print(state.read_text())
 else:
  names=json.loads((root/'assets').read_text())
  if os.environ.get('MOCK_MISSING_ASSET'): names=names[:-1]
  print('\\n'.join(names))
elif a[:2]==['release','create']:
 if os.environ.get('MOCK_UPLOAD_FAIL'): sys.exit(1)
 state.write_text('true')
 (root/'assets').write_text(json.dumps([Path(p).name for p in a if p.startswith('dist/releases/')]))
elif a[:2]==['release','edit']:
 state.write_text('false')
else: sys.exit('unexpected gh call: '+repr(a))
''')
        for path in bindir.iterdir():
            path.chmod(0o755)
        self.env = dict(os.environ, PATH=str(bindir) + os.pathsep + os.environ["PATH"],
                        GH_REPO="fixture/fixture", MOCK_DIR=str(self.root))

    def git(self, *args):
        return subprocess.run(["git", "-C", str(self.repo), *args], check=True, capture_output=True, text=True)

    def release(self, tag="v1.2.3", publish=False, **env):
        return subprocess.run(["bash", str(self.app / "release.sh"), *(["--publish"] if publish else []), tag],
                              env=dict(self.env, **env), capture_output=True, text=True)

    def calls(self):
        log = self.root / "calls"
        return [json.loads(line) for line in log.read_text().splitlines()] if log.exists() else []

    def test_build_only_packages_without_github(self):
        result = self.release()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.calls(), [])
        out = self.app / "dist/releases/v1.2.3"
        lines = (out / "SHA256SUMS").read_text().splitlines()
        self.assertEqual(len(lines), 2)
        for line in lines:
            digest, name = line.split("  ")
            self.assertEqual(digest, hashlib.sha256((out / name).read_bytes()).hexdigest())
        for app_name in ("vrcove-desktop",):
            with tarfile.open(out / f"{app_name}-v1.2.3-linux-amd64.tar.gz") as archive:
                self.assertEqual({Path(n).name for n in archive.getnames()}, {app_name, "README.md", "LICENSE", "BUILD-INFO.txt"})
                executable = next(m for m in archive.getmembers() if m.name.endswith(f"/{app_name}"))
                self.assertEqual(executable.mode, 0o755)
            with zipfile.ZipFile(out / f"{app_name}-v1.2.3-windows-amd64.zip") as archive:
                self.assertEqual({Path(n).name for n in archive.namelist()}, {f"{app_name}.exe", "README.md", "LICENSE", "BUILD-INFO.txt"})
        first = (out / "SHA256SUMS").read_bytes()
        self.assertEqual(self.release().returncode, 0)
        self.assertEqual(first, (out / "SHA256SUMS").read_bytes())

    def test_version_file_is_enforced_and_used_by_default(self):
        (self.repo / "VERSION").write_text("v1.2.3\n")
        self.assertNotEqual(self.release("v9.9.9").returncode, 0)
        res = subprocess.run(["bash", str(self.app / "release.sh")], env=self.env, capture_output=True, text=True)
        self.assertEqual(res.returncode, 0, res.stderr)
        self.assertTrue((self.app / "dist/releases/v1.2.3/SHA256SUMS").exists())

    def test_publish_creates_draft_before_publishing(self):
        result = self.release("v1.2.3-rc.1", publish=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.calls()
        create = next(c for c in calls if c[:2] == ["release", "create"])
        for flag in ["--draft", "--verify-tag", "--prerelease", "--latest=false"]:
            self.assertIn(flag, create)
        self.assertEqual(calls[-1][:2], ["release", "edit"])
        self.assertEqual((self.root / "draft").read_text(), "false")

    def test_missing_asset_keeps_release_as_draft(self):
        result = self.release(publish=True, MOCK_MISSING_ASSET="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((self.root / "draft").read_text(), "true")
        self.assertFalse(any(c[:2] == ["release", "edit"] for c in self.calls()))

    def test_dirty_tree_and_wrong_remote_tag_are_rejected(self):
        (self.repo / "unreviewed.txt").write_text("unreviewed")
        self.assertNotEqual(self.release(publish=True).returncode, 0)
        self.assertEqual(self.calls(), [])
        (self.repo / "unreviewed.txt").unlink()
        self.assertNotEqual(self.release(publish=True, MOCK_WRONG_TAG="1").returncode, 0)
        self.assertFalse(any(c[:2] == ["release", "create"] for c in self.calls()))

    def test_upload_failure_does_not_publish(self):
        self.assertNotEqual(self.release(publish=True, MOCK_UPLOAD_FAIL="1").returncode, 0)
        self.assertFalse(any(c[:2] == ["release", "edit"] for c in self.calls()))

    def test_published_release_is_immutable(self):
        (self.root / "draft").write_text("false")
        self.assertNotEqual(self.release(publish=True).returncode, 0)
        self.assertFalse(any(c[:2] in (["release", "upload"], ["release", "edit"]) for c in self.calls()))


if __name__ == "__main__":
    unittest.main()
