#!/bin/bash

# Define target directory
TARGET_DIR=/opt/bin

# Create target directory if it does not exist
mkdir -p $TARGET_DIR

# Copy rbd.sh
cp "$PWD"/scripts/rbd.sh $TARGET_DIR

# Set exec flag
chmod +x $TARGET_DIR/rbd.sh

# Compile using golang image
docker run --rm -e GOBIN=/usr/src/docker-volume-rdb/bin -v "$PWD"/driver:/usr/src/docker-volume-rdb -w /usr/src/docker-volume-rdb golang:1.4 go get && go build -v 2> error.log

# Copy binary 
cp "$PWD"/driver/bin/docker-volume-rdb $TARGET_DIR

# Set exec flag
chmod +x $TARGET_DIR/docker-volume-rdb

# Copy unit files
cp -r "$PWD"/service/ /etc/systemd/system

# Reload units
systemctl daemon-reload

# Enable unit
systemctl enable docker-rbd-volume-driver.service

# Start unit
systemctl start docker-rbd-volume-driver.service
