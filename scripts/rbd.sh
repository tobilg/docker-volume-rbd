#!/bin/bash
docker run -i --rm \
--privileged \
--pid host \
--net host \
--volume /dev:/dev \
--volume /sys:/sys \
--volume /etc/ceph:/etc/ceph \
--volume /var/lib/ceph:/var/lib/ceph \
h0tbird/ceph:v9.2.0-2 $(basename $0) "$@"