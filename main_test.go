package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

const testImageID = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestRunReusesImage(t *testing.T) {
	dir := fakeDocker(t)
	pwd := filepath.Join(t.TempDir(), "proto files")
	args := []string{"--proto_path=" + pwd, "--go_out=..", "--twirp_out=.", "messages.proto"}
	for range 3 {
		if err := run(pwd, args); err != nil {
			t.Fatal(err)
		}
	}

	calls := dockerCalls(t, dir)
	if len(calls["image"]) != 3 || len(calls["build"]) != 1 || len(calls["run"]) != 3 {
		t.Fatalf("unexpected Docker calls: %v", calls)
	}
	build := calls["build"][0]
	name := build[3]
	wantBuild := []string{"build", "--platform=linux/amd64", "--tag", name, "-q", "-"}
	if !reflect.DeepEqual(build, wantBuild) || !strings.HasPrefix(name, "proto-gen-go:toolchain-") {
		t.Fatalf("build args = %q, want %q with a toolchain tag", build, wantBuild)
	}
	for _, lookup := range calls["image"] {
		want := []string{"image", "ls", "--quiet", "--no-trunc", name}
		if !reflect.DeepEqual(lookup, want) {
			t.Errorf("lookup args = %q, want %q", lookup, want)
		}
	}
	for _, invocation := range calls["run"] {
		want := append([]string{"run", "--rm", "-v", pwd + ":" + pwd, "--platform=linux/amd64", testImageID}, args...)
		if !reflect.DeepEqual(invocation, want) {
			t.Errorf("run args = %q, want %q", invocation, want)
		}
	}
	inputs, err := filepath.Glob(filepath.Join(dir, "call-*", "stdin"))
	if err != nil || len(inputs) != 1 {
		t.Fatalf("build inputs = %v, err = %v", inputs, err)
	}
	input, err := os.ReadFile(inputs[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(input) != dockerfile {
		t.Error("build did not receive the embedded Dockerfile")
	}
}

func TestCompilerImageToolchainChanges(t *testing.T) {
	dir := fakeDocker(t)
	for _, recipe := range []string{dockerfile, dockerfile + "\n", dockerfile} {
		if _, err := compilerImage(recipe); err != nil {
			t.Fatal(err)
		}
	}
	calls := dockerCalls(t, dir)
	if len(calls["build"]) != 2 {
		t.Fatalf("build calls = %v, want two toolchains", calls["build"])
	}
	if calls["build"][0][3] == calls["build"][1][3] {
		t.Error("different Dockerfiles used the same tag")
	}
}

func TestCompilerImageRebuildsMissingImage(t *testing.T) {
	dir := fakeDocker(t)
	if _, err := compilerImage(dockerfile); err != nil {
		t.Fatal(err)
	}
	name := dockerCalls(t, dir)["build"][0][3]
	if err := os.Remove(filepath.Join(dir, "image-"+name)); err != nil {
		t.Fatal(err)
	}
	if _, err := compilerImage(dockerfile); err != nil {
		t.Fatal(err)
	}
	builds := dockerCalls(t, dir)["build"]
	if len(builds) != 2 || !reflect.DeepEqual(builds[0], builds[1]) {
		t.Fatalf("build calls = %v, want two identical tagged builds", builds)
	}
}

func TestRunFailures(t *testing.T) {
	for _, tc := range []struct {
		stage string
		want  string
	}{
		{"lookup", "docker image lookup failed"},
		{"build", "docker build failed"},
		{"run", "protoc command failed"},
	} {
		t.Run(tc.stage, func(t *testing.T) {
			dir := fakeDocker(t)
			t.Setenv("FAKE_DOCKER_FAIL", tc.stage)
			err := run(t.TempDir(), []string{"broken.proto"})
			var exitErr *exec.ExitError
			if err == nil || !strings.Contains(err.Error(), tc.want) || !errors.As(err, &exitErr) {
				t.Fatalf("error = %v, want %q wrapping an exit error", err, tc.want)
			}
			calls := dockerCalls(t, dir)
			if tc.stage != "run" {
				if len(calls["run"]) != 0 {
					t.Fatal("compiler ran after image lookup/build failed")
				}
				if tc.stage != "build" && len(calls["build"]) != 0 {
					t.Fatal("lookup failure was treated as a missing image")
				}
				if tc.stage == "build" {
					t.Setenv("FAKE_DOCKER_FAIL", "")
					if err := run(t.TempDir(), []string{"valid.proto"}); err != nil {
						t.Fatal(err)
					}
					calls = dockerCalls(t, dir)
					if len(calls["build"]) != 2 || len(calls["run"]) != 1 {
						t.Fatal("failed build prevented retry")
					}
				}
				return
			}
			if len(calls["run"]) != 1 || calls["run"][0][1] != "--rm" {
				t.Fatal("failed compiler container was not disposable")
			}
			t.Setenv("FAKE_DOCKER_FAIL", "")
			if err := run(t.TempDir(), []string{"valid.proto"}); err != nil {
				t.Fatal(err)
			}
			calls = dockerCalls(t, dir)
			if len(calls["build"]) != 1 || len(calls["run"]) != 2 {
				t.Fatal("compiler failure prevented subsequent image reuse")
			}
		})
	}
}

func TestCompilerImageRejectsEmptyBuildOutput(t *testing.T) {
	fakeDocker(t)
	t.Setenv("FAKE_DOCKER_BUILD_ID", "")
	if _, err := compilerImage(dockerfile); err == nil || !strings.Contains(err.Error(), "empty image ID") {
		t.Fatalf("error = %v, want empty image ID error", err)
	}
}

func TestRunConcurrent(t *testing.T) {
	dir := fakeDocker(t)
	t.Setenv("FAKE_DOCKER_CACHE_MISS", "1")
	t.Setenv("FAKE_DOCKER_UNIQUE_BUILD_ID", "1")
	// Force cache misses and distinct build IDs to verify execution does not
	// depend on which concurrent build last updates the shared tag.
	const workers = 4
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			if err := run(dir, []string{"messages.proto"}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	calls := dockerCalls(t, dir)
	if len(calls["build"]) != workers || len(calls["run"]) != workers {
		t.Fatalf("unexpected concurrent Docker calls: %v", calls)
	}
	files, err := filepath.Glob(filepath.Join(dir, "call-*", "image"))
	if err != nil {
		t.Fatal(err)
	}
	built := make(map[string]bool)
	for _, file := range files {
		id, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		built[strings.TrimSpace(string(id))] = true
	}
	if len(built) != workers {
		t.Fatalf("distinct build IDs = %d, want %d", len(built), workers)
	}
	for _, invocation := range calls["run"] {
		if invocation[1] != "--rm" || !built[invocation[5]] {
			t.Errorf("unexpected concurrent run args: %q", invocation)
		}
		delete(built, invocation[5])
	}
	if len(built) != 0 {
		t.Errorf("build IDs not used by compiler runs: %v", built)
	}
}

func fakeDocker(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FAKE_DOCKER_DIR", dir)
	t.Setenv("FAKE_DOCKER_FAIL", "")
	t.Setenv("FAKE_DOCKER_CACHE_MISS", "")
	t.Setenv("FAKE_DOCKER_UNIQUE_BUILD_ID", "")
	t.Setenv("FAKE_DOCKER_BUILD_ID", testImageID)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	const script = `#!/bin/sh
set -eu
call="$FAKE_DOCKER_DIR/call-$$"
mkdir "$call"
printf '%s\000' "$@" > "$call/args"
case "$1" in
image)
	if [ "$FAKE_DOCKER_FAIL" = lookup ]; then
		echo "image lookup failed" >&2
		exit 1
	fi
	if [ "$FAKE_DOCKER_CACHE_MISS" != 1 ] && [ -f "$FAKE_DOCKER_DIR/image-$5" ]; then
		cat "$FAKE_DOCKER_DIR/image-$5"
	fi
	;;
build)
	cat > "$call/stdin"
	if [ "$FAKE_DOCKER_FAIL" = build ]; then
		echo "build failed" >&2
		exit 1
	fi
	id="$FAKE_DOCKER_BUILD_ID"
	if [ "$FAKE_DOCKER_UNIQUE_BUILD_ID" = 1 ]; then
		id=$(printf 'sha256:%064d' "$$")
	fi
	printf '%s\n' "$id" > "$call/image"
	cp "$call/image" "$FAKE_DOCKER_DIR/image-$4"
	printf '%s\n' "$id"
	;;
run)
	if [ "$FAKE_DOCKER_FAIL" = run ]; then
		echo "compiler failed" >&2
		exit 1
	fi
	;;
*)
	echo "unexpected Docker command" >&2
	exit 1
	;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func dockerCalls(t *testing.T, dir string) map[string][][]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "call-*", "args"))
	if err != nil {
		t.Fatal(err)
	}
	calls := make(map[string][][]string)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		args := strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
		calls[args[0]] = append(calls[args[0]], args)
	}
	return calls
}
