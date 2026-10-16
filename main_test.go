package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
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
	if len(calls["image"]) != 4 || len(calls["build"]) != 1 || len(calls["run"]) != 3 {
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
		{"recheck", "docker image lookup failed"},
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
						t.Fatal("failed build prevented retry after releasing the lock")
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

func TestCompilerImageLockDirectoryFailure(t *testing.T) {
	dir := fakeDocker(t)
	if err := os.WriteFile(filepath.Join(dir, "not-a-directory"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(dir, "not-a-directory"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "not-a-directory"))
	t.Setenv("LOCALAPPDATA", filepath.Join(dir, "not-a-directory"))
	if _, err := compilerImage(dockerfile); err == nil || !strings.Contains(err.Error(), "create compiler image lock directory") {
		t.Fatalf("error = %v, want lock directory error", err)
	}
	if calls := dockerCalls(t, dir); len(calls["build"]) != 0 {
		t.Fatal("image built without acquiring a lock")
	}
}

func TestRunConcurrent(t *testing.T) {
	dir := fakeDocker(t)
	t.Setenv("FAKE_DOCKER_UNIQUE_BUILD_ID", "1")
	// Each process reports an initial miss before any can build. Any second
	// build gets a different ID, exposing tag replacement rather than hiding it.
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	const workers = 4
	var commands []*exec.Cmd
	var outputs []*bytes.Buffer
	for i := range workers {
		cmd := exec.CommandContext(ctx, binary, "-test.run=^TestRunHelperProcess$")
		cmd.Env = append(os.Environ(),
			"PROTO_GEN_GO_HELPER=1",
			fmt.Sprintf("FAKE_DOCKER_WAITERS=%d", workers),
			fmt.Sprintf("FAKE_DOCKER_FIRST_LOOKUP=%s/first-%d", dir, i),
		)
		output := new(bytes.Buffer)
		cmd.Stdout = output
		cmd.Stderr = output
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
		outputs = append(outputs, output)
	}
	for i, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Errorf("worker %d failed: %v\n%s", i, err, outputs[i])
		}
	}
	calls := dockerCalls(t, dir)
	if len(calls["build"]) != 1 || len(calls["run"]) != workers {
		t.Fatalf("unexpected concurrent Docker calls: %v", calls)
	}
	name := calls["build"][0][3]
	id, err := os.ReadFile(filepath.Join(dir, "image-"+name))
	if err != nil {
		t.Fatal(err)
	}
	for _, invocation := range calls["run"] {
		if invocation[1] != "--rm" || invocation[5] != strings.TrimSpace(string(id)) {
			t.Errorf("unexpected concurrent run args: %q", invocation)
		}
	}
}

func TestRunHelperProcess(t *testing.T) {
	if os.Getenv("PROTO_GEN_GO_HELPER") != "1" {
		return
	}
	if err := run(os.Getenv("FAKE_DOCKER_DIR"), []string{"messages.proto"}); err != nil {
		t.Fatal(err)
	}
}

func fakeDocker(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("FAKE_DOCKER_DIR", dir)
	t.Setenv("FAKE_DOCKER_FAIL", "")
	t.Setenv("FAKE_DOCKER_FIRST_LOOKUP", "")
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
	if [ "$FAKE_DOCKER_FAIL" = recheck ]; then
		if [ -f "$FAKE_DOCKER_DIR/lookup-complete" ]; then
			echo "image recheck failed" >&2
			exit 1
		fi
		touch "$FAKE_DOCKER_DIR/lookup-complete"
	fi
	if [ -n "$FAKE_DOCKER_FIRST_LOOKUP" ] && mkdir "$FAKE_DOCKER_FIRST_LOOKUP" 2>/dev/null; then
		touch "$FAKE_DOCKER_DIR/miss-$$"
		attempt=0
		while :; do
			set -- "$FAKE_DOCKER_DIR"/miss-*
			[ "$#" -eq "$FAKE_DOCKER_WAITERS" ] && break
			attempt=$((attempt + 1))
			if [ "$attempt" -ge 500 ]; then
				echo "timed out waiting for concurrent lookups" >&2
				exit 1
			fi
			sleep 0.01
		done
		exit 0
	fi
	if [ -f "$FAKE_DOCKER_DIR/image-$5" ]; then
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
