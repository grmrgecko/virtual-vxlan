#!/bin/env bash

# Ensure we're in this directory when running the script.
cd -P -- "$(dirname -- "$0")" || exit 1

# Remove existing builds.
rm -Rf ./linux_*/

# Build the different sysroots in docker.
if ! docker buildx build --platform=linux/amd64,linux/arm64 --tag 'cross-sysroot:latest' --output 'type=local,dest=.' .; then
    echo "Failed to build sysroot, please review sysroot/README.md for instructions on configuring environment."
    exit 1
fi

# Setup exclude and include list for minimal sysroot building.
exclude_list=()
include_list=()

exclude_list+=(--exclude "/bin")
exclude_list+=(--exclude "/boot")
exclude_list+=(--exclude "/boot*")
exclude_list+=(--exclude "/dev")
exclude_list+=(--exclude "/etc")
exclude_list+=(--exclude "/home")
exclude_list+=(--exclude "/lib/dhcpd")
exclude_list+=(--exclude "/lib/firmware")
exclude_list+=(--exclude "/lib/hdparm")
exclude_list+=(--exclude "/lib/ifupdown")
exclude_list+=(--exclude "/lib/modules")
exclude_list+=(--exclude "/lib/modprobe.d")
exclude_list+=(--exclude "/lib/modules-load.d")
exclude_list+=(--exclude "/lib/resolvconf")
exclude_list+=(--exclude "/lib/startpar")
exclude_list+=(--exclude "/lib/systemd")
exclude_list+=(--exclude "/lib/terminfo")
exclude_list+=(--exclude "/lib/udev")
exclude_list+=(--exclude "/lib/xtables")
exclude_list+=(--exclude "/lib/ssl/private")
exclude_list+=(--exclude "/lost+found")
exclude_list+=(--exclude "/media")
exclude_list+=(--exclude "/mnt")
exclude_list+=(--exclude "/proc")
exclude_list+=(--exclude "/root")
exclude_list+=(--exclude "/run")
exclude_list+=(--exclude "/sbin")
exclude_list+=(--exclude "/srv")
exclude_list+=(--exclude "/sys")
exclude_list+=(--exclude "/tmp")
exclude_list+=(--exclude "/usr/bin")
exclude_list+=(--exclude "/usr/games")
exclude_list+=(--exclude "/usr/sbin")
exclude_list+=(--exclude "/usr/share")
exclude_list+=(--exclude "/usr/src")
exclude_list+=(--exclude "/usr/local/bin")
exclude_list+=(--exclude "/usr/local/etc")
exclude_list+=(--exclude "/usr/local/games")
exclude_list+=(--exclude "/usr/local/man")
exclude_list+=(--exclude "/usr/local/sbin")
exclude_list+=(--exclude "/usr/local/share")
exclude_list+=(--exclude "/usr/local/src")
exclude_list+=(--exclude "/usr/lib/ssl/private")
exclude_list+=(--exclude "/var")
exclude_list+=(--exclude "/snap")
exclude_list+=(--exclude "*python*")

include_list+=(--include "*.a")
include_list+=(--include "*.so")
include_list+=(--include "*.so.*")
include_list+=(--include "*.h")
include_list+=(--include "*.hh")
include_list+=(--include "*.hpp")
include_list+=(--include "*.hxx")
include_list+=(--include "*.pc")
include_list+=(--include "/lib")
include_list+=(--include "/lib32")
include_list+=(--include "/lib64")
include_list+=(--include "/libx32")
include_list+=(--include "*/")

args=()
args+=(-a)
args+=(-z)
args+=(-m)
args+=(-d)
args+=(-h)
args+=(--keep-dirlinks)
args+=("--info=progress2")
args+=(--delete)
args+=(--prune-empty-dirs)
args+=(--sparse)
args+=(--links)
args+=(--copy-unsafe-links)
args+=("${exclude_list[@]}")
args+=("${include_list[@]}")
args+=(--exclude "*")

# Make the sysroot environments minimal.
echo "Making sysroot the bare minimal."
for arch in linux_*; do
    # Skip already minimal dirs.
    if [[ $arch =~ _minimal ]]; then
        continue
    fi

    # Fix symbolic links.
    (
        cd "$arch" || exit 1
        for slink in ./usr/lib64/ld-linux-x86-64.so.2 ./usr/lib/*-linux-gnu/*.so; do
            if [[ -L $slink ]]; then
                spath=$(readlink "$slink")
                if grep -q "/${arch}/" <<<"$spath"; then
                    npath=$(sed "s/\/$arch\//.\//" <<<"$spath")
                    rpath=$(realpath -m --relative-to="$(dirname "$slink")" "$npath")
                    unlink "$slink"
                    ln -s "$rpath" "$slink"
                fi
            fi
        done
    )

    # Run rsync to minimal.
    rsync "${args[@]}" "$arch/" "${arch}_minimal/"
    rm -Rf "${arch:?}/"
    mv "${arch}_minimal/" "$arch/"
done

# Clear build cache.
echo "Clearing build cache."
docker buildx prune --all --force

echo "Done building sysroot."
