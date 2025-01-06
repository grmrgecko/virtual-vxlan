# Building sysroot

- Install qemu binfmt support, and install docker with the buildx extension.

- Ensure qemu is registered with:
```bash
ls /proc/sys/fs/binfmt_misc/
```

- Setup buildx:
```bash
docker buildx create --name mybuilder --use
```

- Build the sysroot:
```bash
./build.sh
```
