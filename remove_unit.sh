#!/bin/bash

# Define target directory
TARGET_DIR=/opt/bin

# Stop service
systemctl stop docker-rbd-volume-driver.service

# Remove service
systemctl disable docker-rbd-volume-driver.service

# Reload
systemctl daemon-reload

# Delete service files
rm /etc/systemd/system/docker-rbd-volume-driver.service
rm -rf /etc/systemd/system/docker-rbd-volume-driver.service.d/
