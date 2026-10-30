#!/usr/bin/env python3
"""Private test-only RED/GREEN observer injection; never edits its source tree."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import time

API_TEST = Path("internal/webserver/api/Api_test.go")
HELPERS = r'''

var m5DeletionObservation = database.ReadRecoveryFile

func m5InstallControlledDeletion(t *testing.T) {
	t.Helper()
	const id = "smalltestfileDelay"
	lockCtx, lockCancel := context.WithTimeout(context.Background(), 3*time.Second)
	unlock, err := apimutex.LockContext(lockCtx, apimutex.TypeMetaData, id)
	lockCancel()
	if err != nil {
		t.Fatal("M5_SETUP_FAILED acquire delayed-file metadata gate", err)
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(unlock) }
	t.Cleanup(release)
	readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
	snapshot, found, err := database.ReadRecoveryFile(readCtx, id)
	readCancel()
	if err != nil || !found || snapshot.PendingDeletion == 0 || storage.IsExpiredFile(snapshot, time.Now().Unix()) {
		release()
		t.Fatalf("M5_SETUP_FAILED expected live pending row: found=%v pending=%d error=%v", found, snapshot.PendingDeletion, err)
	}
	installed := time.Now()
	eligible := installed.Add(1100 * time.Millisecond)
	source := filepath.Join(configuration.Get().DataDir, snapshot.SHA1)
	previous := m5DeletionObservation
	releasedByObservation := false
	m5DeletionObservation = func(ctx context.Context, candidate string) (models.File, bool, error) {
		file, exists, readErr := previous(ctx, candidate)
		// Both versions retain exactly this fault until their first observation
		// after 1.1s. Capture first, then release: the RED result cannot race GC.
		if candidate == id && !releasedByObservation && !time.Now().Before(eligible) {
			releasedByObservation = true
			t.Logf("M5_OBSERVER_CAPTURE elapsed_ms=%d found=%v pending=%d error=%v", time.Since(installed).Milliseconds(), exists, file.PendingDeletion, readErr)
			release()
		}
		return file, exists, readErr
	}
	t.Log("M5_SETUP_OK live pending metadata gate held")
	t.Cleanup(func() {
		m5DeletionObservation = previous
		release()
		if !releasedByObservation {
			t.Error("M5_SETUP_FAILED no qualified completion observation")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			_, exists, readErr := database.ReadRecoveryFile(ctx, id)
			if readErr != nil {
				t.Error("M5_DRAIN_FAILED read current deletion state", readErr)
				return
			}
			if !exists {
				// Wait for the callback to leave its metadata gate before fixture
				// teardown can close this database. Its remaining finish is memory-only.
				drainedUnlock, gateErr := apimutex.LockContext(ctx, apimutex.TypeMetaData, id)
				if gateErr != nil {
					t.Error("M5_DRAIN_FAILED callback still holds metadata gate", gateErr)
					return
				}
				drainedUnlock()
				shared, referenceErr := database.HasBlobReference(ctx, snapshot)
				_, sourceErr := os.Lstat(source)
				if referenceErr != nil || (shared && sourceErr != nil) || (!shared && !os.IsNotExist(sourceErr)) {
					t.Errorf("M5_DRAIN_FAILED retired metadata/source disagreement: shared=%v reference_error=%v source_error=%v", shared, referenceErr, sourceErr)
					return
				}
				t.Logf("M5_DRAIN_OK metadata_absent=true shared_source=%v", shared)
				return
			}
			select {
			case <-ticker.C:
			case <-ctx.Done():
				t.Error("M5_DRAIN_FAILED callback did not retire metadata after release", ctx.Err())
				return
			}
		}
	})
}
'''


def digest(data):
    return hashlib.sha256(data).hexdigest()


def function_span(source, name):
    start = source.index(f"func {name}(")
    end = source.find("\nfunc ", start + 1)
    return start, len(source) if end < 0 else end


def inject(text, mode):
    start, end = function_span(text, "TestDeleteFile")
    test = text[start:end]
    anchor = "\tremoveUserPermission(t, idUser, models.UserPermDeleteOtherUploads)\n" if mode == "red" else '\twaitForDeletedMetadata(t, "smalltestfileDelay")\n'
    if test.count(anchor) != 1:
        raise ValueError("TestDeleteFile common injection anchor is not unique")
    replacement = anchor + "\tm5InstallControlledDeletion(t)\n" if mode == "red" else "\tm5InstallControlledDeletion(t)\n" + anchor
    test = test.replace(anchor, replacement, 1)
    if mode == "red":
        old = '\t_, ok = database.GetMetaDataById("smalltestfileDelay")\n\ttest.IsEqualBool(t, ok, false)'
        new = '\t_, ok, m5ObservedErr := m5DeletionObservation(context.Background(), "smalltestfileDelay")\n\tif m5ObservedErr != nil { t.Fatal("M5_OBSERVER_READ_FAILED", m5ObservedErr) }\n\ttest.IsEqualBool(t, ok, false)'
        if test.count(old) != 1:
            raise ValueError("Original final one-shot completion assertion is missing")
        test = test.replace(old, new, 1)
    text = text[:start] + test + text[end:]
    if mode == "green":
        start, end = function_span(text, "waitForDeletedMetadata")
        helper = text[start:end]
        old = "database.ReadRecoveryFile(ctx, id)"
        if helper.count(old) != 1:
            raise ValueError("GREEN checked completion helper read is not unique")
        text = text[:start] + helper.replace(old, "m5DeletionObservation(ctx, id)", 1) + text[end:]
    for line in ['"sync"', '"github.com/Activer007/SupaShare/internal/webserver/api/mutex/apimutex"']:
        if line not in text:
            text = text.replace("import (\n", "import (\n\t" + line + "\n", 1)
    return text + HELPERS


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", required=True)
    parser.add_argument("--dest", required=True)
    parser.add_argument("--evidence", required=True)
    parser.add_argument("--mode", choices=["red", "green"], required=True)
    parser.add_argument("--green-api")
    parser.add_argument("--run", action="store_true")
    args = parser.parse_args()
    source, dest = Path(args.source).resolve(), Path(args.dest).resolve()
    evidence = Path(args.evidence).resolve()
    if source == dest or source in dest.parents or dest.exists():
        raise ValueError("Destination must be absent and separate from the source")
    if args.mode == "green" and not args.green_api:
        raise ValueError("GREEN requires the reviewed API test source")
    baseline = (source / API_TEST).read_bytes()
    overlay = Path(args.green_api).read_bytes() if args.green_api else baseline
    shutil.copytree(source, dest, symlinks=True)
    (dest / API_TEST).write_text(inject(overlay.decode("utf-8").replace("\r\n", "\n"), args.mode), encoding="utf-8", newline="\n")
    subprocess.run(["gofmt", "-w", str(dest / API_TEST)], check=True)
    evidence.mkdir(parents=True, exist_ok=True)
    command = ["go", "test", "./internal/webserver/api", "-tags=test,noaws", "-run", "^TestDeleteFile$", "-count=1", "-timeout=30s", "-v"]
    manifest = {
        "baseline_commit": "77b8fa2c729db05a5e2ae3434b50f02eda127def",
        "mode": args.mode, "source": str(source), "destination": str(dest),
        "injector_sha256": digest(Path(__file__).read_bytes()),
        "baseline_api_test_sha256": digest(baseline), "overlay_api_test_sha256": digest(overlay),
        "injected_api_test_sha256": digest((dest / API_TEST).read_bytes()),
        "command": command,
        "go_version": subprocess.check_output(["go", "version"], cwd=dest, text=True).strip(),
    }
    if args.run:
        started = time.monotonic()
        with (evidence / f"{args.mode}.log").open("w", encoding="utf-8") as output:
            result = subprocess.run(command, cwd=dest, stdout=output, stderr=subprocess.STDOUT, env=os.environ.copy())
        manifest.update(exit_code=result.returncode, elapsed_seconds=round(time.monotonic()-started, 3))
        log = (evidence / f"{args.mode}.log").read_text()
        manifest["markers"] = {marker: marker in log for marker in ["M5_SETUP_OK", "M5_OBSERVER_CAPTURE", "M5_DRAIN_OK"]}
        print(log)
    (evidence / f"{args.mode}-manifest.json").write_text(json.dumps(manifest, indent=2)+"\n", encoding="utf-8")
    print(json.dumps(manifest, indent=2))
    return manifest.get("exit_code", 0)


if __name__ == "__main__":
    sys.exit(main())
