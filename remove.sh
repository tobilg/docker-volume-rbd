#!/bin/bash

# Define target directory
TARGET_DIR=/opt/bin

# Stop service
systemctl stop docker-rbd-volume-driver.service

# Remove service
systemctl disable docker-rbd-volume-driver.service

# Delete service files
rm -rf /etc/systemd/system/docker-rbd-volume-driver.service
rm -rf /etc/systemd/system/docker-rbd-volume-driver.service.d/

# Delete files in target directory
#rm -rf $TARGET_DIR