#!/bin/sh
# Run the test suite in containers, one per distro / git version.
#   test/docker/run.sh                 # all targets
#   test/docker/run.sh ubuntu:22.04    # a distro and the git it ships
#   test/docker/run.sh git:2.30.2      # an exact git version, built from source
#   PLATFORM=linux/amd64 test/docker/run.sh
set -eu
cd "$(dirname "$0")/../.."

targets=${*:-"git:2.28.0 git:2.30.2 ubuntu:22.04 debian:12 ubuntu:24.04 alpine:3.20 fedora:41 git:2.55.0"}
platform=${PLATFORM:+--platform $PLATFORM}
failed=""

for base in $targets; do
	tag="kis-test:$(echo "$base" | tr ':/' '--')"
	case $base in
	git:*) args="--build-arg GIT_VERSION=${base#git:}" ;;
	*) args="--build-arg BASE=$base" ;;
	esac
	echo "==> $base"
	if docker build -q $platform $args -t "$tag" -f test/docker/Dockerfile . >/dev/null &&
		docker run --rm $platform "$tag"; then
		echo "==> $base: ok"
	else
		echo "==> $base: FAILED"
		failed="$failed $base"
	fi
done

if [ -n "$failed" ]; then
	echo "failed:$failed"
	exit 1
fi
echo "all targets passed"
