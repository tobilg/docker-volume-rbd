# docker-volume-rbd
A Docker volume driver for RBD (mainly for CoreOS)

## Installation

The volume driver can be installed as `systemd` unit with the `install.sh` script. It will compile the driver via the `golang:1.4` image and place the resulting `docker-volume-rbd` driver in `/opt/bin`. 
Furthermore, it copies the `rbd` script to `/opt/bin` as well.

### Details

The following steps should be taken to install the RBD volume driver:

#### Enable `rbd` kernel module

```
core@core-1 ~ $ modprobe rbd
```

#### Clone the driver 

```
core@core-1 ~ $ cd /tmp
core@core-1 /tmp $ git clone https://github.com/tobilg/docker-volume-rbd.git
```

#### Run installation script

```
core@core-1 /tmp $ cd docker-volume-rbd
core@core-1 /tmp/docker-volume-rbd $ chmod +x install.sh
core@core-1 /tmp/docker-volume-rbd $ sudo ./install.sh
```

#### Finished!

## Uninstallation

The provided `remove_unit.sh` script can be used to remove the `systemd` unit. The `/opt/bin` needs to be deleted manually if desired.

## Usage examples

Client side:
```
core@core-1 ~ $ docker run -it --volume-driver rbd -v foo:/foo alpine /bin/sh -c "echo -n 'hello ' > /foo/hw.txt"
core@core-1 ~ $ docker run -it --volume-driver rbd -v foo:/foo alpine /bin/sh -c "echo world >> /foo/hw.txt"
core@core-1 ~ $ docker run -it --volume-driver rbd -v foo:/foo alpine cat /foo/hw.txt
hello world
```

The last command should also produce the same result on all host where you installed `docker-volume-rbd`:
```
core@core-2 ~ $ docker run -it --volume-driver rbd -v foo:/foo alpine cat /foo/hw.txt
hello world
```

Server side:
```
core@core-1 ~ $ sudo journalctl -u docker-rbd-volume-driver.service
Jan 26 09:34:39 core-1 systemd[1]: Started Docker RBD volume driver.
Jan 26 09:34:39 core-1 docker-volume-rdb[2802]: 2016/01/26 09:34:39 main.go:97: [Init] INFO volume root is /var/lib/docker-volumes/rbd
Jan 26 09:34:39 core-1 docker-volume-rdb[2802]: 2016/01/26 09:34:39 driver.go:83: [Init] INFO loading RBD kernel module...
Jan 26 09:34:39 core-1 docker-volume-rdb[2802]: 2016/01/26 09:34:39 main.go:102: [Init] INFO listening on /var/run/docker/plugins/rbd.sock
Jan 26 09:35:50 core-1 docker-volume-rdb[2802]: 2016/01/26 09:35:50 driver.go:212: [Mount] INFO locking image foo
Jan 26 09:35:51 core-1 docker-volume-rdb[2802]: 2016/01/26 09:35:51 driver.go:220: [Mount] INFO mapping image foo
Jan 26 09:35:51 core-1 docker-volume-rdb[2802]: 2016/01/26 09:35:51 driver.go:230: [Mount] INFO creating /var/lib/docker-volumes/rbd/rbd/foo
Jan 26 09:35:51 core-1 docker-volume-rdb[2802]: 2016/01/26 09:35:51 driver.go:240: [Mount] INFO mounting device /dev/rbd0
Jan 26 09:35:52 core-1 docker-volume-rdb[2802]: 2016/01/26 09:35:52 driver.go:293: [Unmount] INFO unmounting device /dev/rbd0
Jan 26 09:35:52 core-1 docker-volume-rdb[2802]: 2016/01/26 09:35:52 driver.go:300: [Unmount] INFO unmapping image foo
Jan 26 09:35:52 core-1 docker-volume-rdb[2802]: 2016/01/26 09:35:52 driver.go:307: [Unmount] INFO unlocking image foo
```

## Further info for CoreOS

### `rbd` command

#### With Docker wrapper:

If you are a CoreOS user you must provide a way to run the `rbd` command. The `install.sh` script will create a wrapper script around the `ceph` Docker image at `/opt/bin/rbd`

#### With `systemd-nspawn`:

If you prefer to use `systemd-nspawn` you need to replace the script located at `/opt/bin/rbd` with the one below:

```
core@core-1 ~ $ cat /opt/bin/rbd
#!/bin/bash

readonly CEPH_DOCKER_IMAGE=h0tbird/ceph
readonly CEPH_DOCKER_TAG=v9.2.0-2
readonly CEPH_USER=root

machinename=$(echo "${CEPH_DOCKER_IMAGE}-${CEPH_DOCKER_TAG}" | sed -r 's/[^a-zA-Z0-9_.-]/_/g')
machinepath="/var/lib/toolbox/${machinename}"
osrelease="${machinepath}/etc/os-release"

[ -f ${osrelease} ] || {
  sudo mkdir -p "${machinepath}"
  sudo chown ${USER}: "${machinepath}"
  docker pull "${CEPH_DOCKER_IMAGE}:${CEPH_DOCKER_TAG}"
  docker run --name=${machinename} "${CEPH_DOCKER_IMAGE}:${CEPH_DOCKER_TAG}" /bin/true
  docker export ${machinename} | sudo tar -x -C "${machinepath}" -f -
  docker rm ${machinename}
  sudo touch ${osrelease}
}

[ "$1" == 'dryrun' ] || {
  sudo systemd-nspawn \
  --quiet \
  --directory="${machinepath}" \
  --capability=all \
  --share-system \
  --bind=/dev:/dev \
  --bind=/etc/ceph:/etc/ceph \
  --bind=/var/lib/ceph:/var/lib/ceph \
  --user="${CEPH_USER}" \
  --setenv=CMD="$(basename $0)" \
  --setenv=ARG="$*" \
  /bin/bash -c '\
  mount -o remount,rw -t sysfs sysfs /sys; \
  $CMD $ARG'
}
```
