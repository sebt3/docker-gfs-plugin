# docker gfs plugin
Docker Volume Driver for gfs volumes

This plugin can be used to create gfs volumes of specified size, which can
then be bind mounted into the container using `docker run` command.

## Requierements
All the nodes needs to have a working gfs setup with dlm and corosync configured correctly.


## Build instruction

    1) git clone https://github.com/sebt3/docker-gfs-plugin.git
    2) cd docker-gfs-plugin
    3) export GO111MODULE=on
    4) make
    alternatively:
    4) docker run --rm -v "$PWD":/usr/src/plug -w /usr/src/plug -e GO111MODULE=on golang:buster go build -o docker-gfs-plugin .
    5) sudo make install


## Usage

1) Start the docker daemon before starting the docker-gfs-plugin daemon.
   You can start docker daemon using command:
```bash
sudo systemctl start docker
```
2) Once docker daemon is up and running, you can start docker-gfs-plugin daemon
   using command:
```bash
sudo systemctl start docker-gfs-plugin
```
NOTE: docker-gfs-plugin daemon is on-demand socket activated. Running `docker volume ls` command
will automatically start the daemon.

3) Since gfs require lvmlockd, it is the responsibility of the user (administrator)
   to configure this daemon correctly.

4) Since logical volumes (lv's) are based on a volume group, it is the
   responsibility of the user (administrator) to provide a volume group name.
   You can choose an existing volume group name by listing volume groups on
   your system using `vgs` command OR create a new volume group using
   `vgcreate` command.
   e.g.
```bash
vgcreate --shared vg0 /dev/hda
```
   where /dev/hda is your partition or whole disk on which physical volumes
   were created.

5) Since you'll probably run this on a swarm cluster, you'll want to have the 
   plugin data shared between nodes. and since you have a gfs2 cluster available,
   use it:
```bash
lvcreate -asy -L 150M --name plugin-config vg0
mkfs.gfs2 -t mycluster:plugin-config -j 3 /dev/vg0/plugin-config
mkdir -p /var/lib/docker-gfs-plugin
mount -t gfs2 -o noatime /dev/vg0/plugin-config /var/lib/docker-gfs-plugin
```

6) Add this volume group name and the lvmlockd cluster name in the config file:
```bash
/etc/docker/docker-gfs-plugin
```

7) The docker-gfs-plugin allows you to create volumes using an optional volume group, which you can pass using `--opt vg` in `docker volume create` command. However, this is **not recommended** and user (administrator) should stick to the default volume group specified in /etc/docker/docker-gfs-plugin config file.

   If a user still chooses to create a volume using an optional volume group
   e.g `--opt vg=vg1`, user **must** pass `--opt vg=vg1` when creating any derivative volumes
   based off this original volume. E.g

## Volume Creation
`docker volume create` command supports the creation of regular gfs volumes, thin volumes, snapshots of regular and thin volumes.

Usage: docker volume create [OPTIONS]
```bash
-d, --driver    string    Specify volume driver name (default "local")
--label         list      Set metadata for a volume (default [])
--name          string    Specify volume name
-o, --opt       map       Set driver specific options (default map[])
```
Following options can be passed using `-o` or `--opt`
```bash
--opt size
--opt keyfile
--opt vg
--opt node_count
--opt cluster_name
```
Please see examples below on how to use these options.

## Examples
```bash
$ docker volume create -d gfs --opt size=0.2G --name foobar
```
This will create a gfs volume named `foobar` of size 208 MB (0.2 GB) in the
volume group vg0.
```bash
$ docker volume create -d gfs --opt size=0.2G --opt vg=vg1 --name foobar
```
This will create a gfs volume named `foobar` of size 208 MB (0.2 GB) in the
volume group vg1.
```bash
docker volume create -d gfs --opt size=0.2G --opt keyfile=/var/lib/docker-gfs-plugin/key.bin --name crypt_vol
```
This will create a LUKS encrypted gfs volume named `crypt_vol` with the contents of `/var/lib/docker-gfs-plugin/key.bin` as a binary passphrase. Snapshots of encrypted volumes use the same key file. The key file must be present when the volume is created, and when it is mounted to a container. Since the file should be found on all nodes, using a shared FS sound like a good idea.

## Volume List
Use `docker volume ls --help` for more information.

``` bash
$ docker volume ls
```
This will list volumes created by all docker drivers including the default driver (local).

## Volume Inspect
Use `docker volume inspect --help` for more information.

``` bash
$ docker volume inspect foobar
```
This will inspect `foobar` and return a JSON.
```bash
[
    {
        "Driver": "gfs",
        "Labels": {},
        "Mountpoint": "/var/lib/docker-gfs-plugin/foobar",
        "Name": "foobar",
        "Options": {
            "size": "0.2G"
        },
        "Scope": "global"
    }
]
```

## Volume Removal
Use `docker volume rm --help` for more information.
```bash
$ docker volume rm foobar
```
This will remove gfs volume `foobar`.

## Bind Mount gfs volume inside the container

```bash
$ docker run -it -v foobar:/home fedora /bin/bash
```
This will bind mount the logical volume `foobar` into the home directory of the container.

## Currently supported environments.
Fedora, RHEL, Centos, Ubuntu (>= 16.04)

## License
GNU GPL
