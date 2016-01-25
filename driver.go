//-----------------------------------------------------------------------------
// Package membership:
//-----------------------------------------------------------------------------

package main

//-----------------------------------------------------------------------------
// Imports:
//-----------------------------------------------------------------------------

import (

	// Standard library:
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	// Community:
	"github.com/docker/go-plugins-helpers/volume"
)

//-----------------------------------------------------------------------------
// Package constant declarations:
//-----------------------------------------------------------------------------

const lockID = "dockerLock"

//-----------------------------------------------------------------------------
// Package variable declarations factored into a block:
//-----------------------------------------------------------------------------

var (
	commands  = [...]string{"modprobe", "rbd", "mount", "umount"}
	nameRegex = regexp.MustCompile(`^(([-_.[:alnum:]]+)/)?([-_.[:alnum:]]+)(@([0-9]+))?$`)
	lockRegex = regexp.MustCompile(`^(client.[0-9]+) ` + lockID)
)

//-----------------------------------------------------------------------------
// Structs definitions:
//-----------------------------------------------------------------------------

type _volume struct {
	name   string
	device string
	locker string
	fstype string
	pool   string
}

type rbdDriver struct {
	volRoot   string
	defPool   string
	defFsType string
	defSize   int
	cmd       map[string]string
	volumes   map[string]*volume
}

//-----------------------------------------------------------------------------
// initDriver
//-----------------------------------------------------------------------------

func initDriver(volRoot, defPool, defFsType string, defSize int) rbdDriver {

	// Variables
	var err error
	cmd := make(map[string]string)

	// Search for binaries
	for _, i := range commands {
		cmd[i], err = exec.LookPath(i)
		if err != nil {
			log.Fatal("[Init] ERROR make sure binary %s is in your PATH", i)
		}
	}

	// Load RBD kernel module
	log.Printf("[Init] INFO loading RBD kernel module...")
	if err = exec.Command(cmd["modprobe"], "rbd").Run(); err != nil {
		log.Fatal("[Init] ERROR unable to load RBD kernel module")
	}

	// Initialize the struct
	driver := rbdDriver{
		volRoot:   volRoot,
		defPool:   defPool,
		defFsType: defFsType,
		defSize:   defSize,
		cmd:       cmd,
		volumes:   map[string]*_volume{},
	}

	return driver
}

//-----------------------------------------------------------------------------
// POST /VolumeDriver.Create
//
// Request:
//  { "Name": "volume_name" }
//  Instruct the plugin that the user wants to create a volume, given a user
//  specified volume name. The plugin does not need to actually manifest the
//  volume on the filesystem yet (until Mount is called).
//
// Response:
//  { "Err": null }
//  Respond with a string error if an error occurred.
//-----------------------------------------------------------------------------

func (d *rbdDriver) Create(r volume.Request) volume.Response {

	// Parse the docker --volume option
	pool, name, size, err := d.parsePoolNameSize(r.Name)
	if err != nil {
		log.Printf("[Create] ERROR parsing volume: %s", err)
		return volume.Response{Err: err.Error()}
	}

	// Check if volume already exists
	mountpoint := filepath.Join(d.volRoot, pool, name)
	if _, found := d.volumes[mountpoint]; found {
		log.Println("[Create] INFO volume is already in known mounts: " + mountpoint)
		return volume.Response{}
	}

	// Create RBD image if not exists
	if exists, err := d.imageExists(pool, name); !exists && err == nil {
		log.Println("[Create] INFO image does not exists. Creating it now...")
		if err = d.createImage(pool, name, d.defFsType, size); err != nil {
			return volume.Response{Err: err.Error()}
		}
	} else if err != nil {
		log.Printf("[Create] ERROR checking for RBD Image: %s", err)
		return volume.Response{Err: err.Error()}
	}

	return volume.Response{}
}

//-----------------------------------------------------------------------------
// POST /VolumeDriver.Remove
//
// Request:
//  { "Name": "volume_name" }
//  Delete the specified volume from disk. This request is issued when a user
//  invokes docker rm -v to remove volumes associated with a container.
//
// Response:
//  { "Err": null }
//  Respond with a string error if an error occurred.
//-----------------------------------------------------------------------------

func (d *rbdDriver) Remove(r volume.Request) volume.Response {
	return volume.Response{}
}

//-----------------------------------------------------------------------------
// POST /VolumeDriver.Path
//
// Request:
//  { "Name": "volume_name" }
//  Docker needs reminding of the path to the volume on the host.
//
// Response:
//  { "Mountpoint": "/path/to/directory/on/host", "Err": null }
//  Respond with the path on the host filesystem where the volume has been
//  made available, and/or a string error if an error occurred.
//-----------------------------------------------------------------------------

func (d *rbdDriver) Path(r volume.Request) volume.Response {

	// Parse the docker --volume option
	pool, name, _, err := d.parsePoolNameSize(r.Name)
	if err != nil {
		log.Printf("[Path] ERROR parsing volume: %s", err)
		return volume.Response{Err: err.Error()}
	}

	mountpoint := filepath.Join(d.volRoot, pool, name)
	return volume.Response{Mountpoint: mountpoint}
}

//-----------------------------------------------------------------------------
// POST /VolumeDriver.Mount
//
// Request:
//  { "Name": "volume_name" }
//  Docker requires the plugin to provide a volume, given a user specified
//  volume name. This is called once per container start.
//
// Response:
//  { "Mountpoint": "/path/to/directory/on/host", "Err": null }
//  Respond with the path on the host filesystem where the volume has been
//  made available, and/or a string error if an error occurred.
//-----------------------------------------------------------------------------

func (d *rbdDriver) Mount(r volume.Request) volume.Response {

	// Parse the docker --volume option
	pool, name, _, err := d.parsePoolNameSize(r.Name)
	if err != nil {
		log.Printf("[Mount] ERROR parsing volume: %s", err)
		return volume.Response{Err: err.Error()}
	}

	// Add image lock
	log.Printf("[Mount] INFO locking image %s", name)
	locker, err := d.lockImage(pool, name, lockID)
	if err != nil {
		log.Printf("[Mount] ERROR locking image: %s", err)
		return volume.Response{Err: err.Error()}
	}

	// Map the image to a kernel device
	log.Printf("[Mount] INFO mapping image %s", name)
	device, err := d.mapImage(pool, name)
	if err != nil {
		defer d.unlockImage(pool, name, lockID, locker)
		log.Printf("[Mount] ERROR mapping image: %s", err)
		return volume.Response{Err: err.Error()}
	}

	// Create mountpoint
	mountpoint := filepath.Join(d.volRoot, pool, name)
	log.Printf("[Mount] INFO creating %s", mountpoint)
	err = os.MkdirAll(mountpoint, os.ModeDir|os.FileMode(int(0775)))
	if err != nil {
		defer d.unmapImage(device)
		defer d.unlockImage(pool, name, lockID, locker)
		log.Printf("[Mount] ERROR creating mount point: %s", err)
		return volume.Response{Err: err.Error()}
	}

	// Mount the device
	log.Printf("[Mount] INFO mounting device %s", device)
	if err = d.mountDevice(device, mountpoint, d.defFsType); err != nil {
		defer d.unmapImage(device)
		defer d.unlockImage(pool, name, lockID, locker)
		log.Printf("[Mount] ERROR mounting device: %s", err)
		return volume.Response{Err: err.Error()}
	}

	// Add to list of volumes
	d.volumes[mountpoint] = &_volume{
		name:   name,
		device: device,
		locker: locker,
		fstype: d.defFsType,
		pool:   pool,
	}

	return volume.Response{Mountpoint: mountpoint}
}

//-----------------------------------------------------------------------------
// POST /VolumeDriver.Unmount
//
// Request:
//  { "Name": "volume_name" }
//  Indication that Docker no longer is using the named volume. This is called
//  once per container stop. Plugin may deduce that it is safe to deprovision
//  it at this point.
//
// Response:
//  { "Err": null }
//  Respond with a string error if an error occurred.
//-----------------------------------------------------------------------------

func (d *rbdDriver) Unmount(r volume.Request) volume.Response {

	// Parse the docker --volume option
	pool, name, _, err := d.parsePoolNameSize(r.Name)
	if err != nil {
		log.Printf("[Unmount] ERROR parsing volume: %s", err)
		return volume.Response{Err: err.Error()}
	}

	// Retrieve volume state
	mountpoint := filepath.Join(d.volRoot, pool, name)
	vol, found := d.volumes[mountpoint]
	if !found {
		err = errors.New("No state found")
		log.Printf("[Unmount] ERROR retrieving state: %s", err)
		return volume.Response{Err: err.Error()}
	}

	// Unmount the device
	log.Printf("[Unmount] INFO unmounting device %s", vol.device)
	if err := d.unmountDevice(vol.device); err != nil {
		log.Printf("[Unmount] ERROR unmounting device: %s", err)
		return volume.Response{Err: err.Error()}
	}

	// Unmap the image
	log.Printf("[Unmount] INFO unmapping image %s", name)
	if err = d.unmapImage(vol.device); err != nil {
		log.Printf("[Unmount] ERROR unmapping image: %s", err)
		return volume.Response{Err: err.Error()}
	}

	// Unlock the image
	log.Printf("[Unmount] INFO unlocking image %s", name)
	if err = d.unlockImage(vol.pool, vol.name, lockID, vol.locker); err != nil {
		log.Printf("[Unmount] ERROR unlocking image: %s", err)
		return volume.Response{Err: err.Error()}
	}

	// Forget the volume
	delete(d.volumes, mountpoint)
	return volume.Response{}
}

//-----------------------------------------------------------------------------
// parsePoolNameSize
//-----------------------------------------------------------------------------

func (d *rbdDriver) parsePoolNameSize(src string) (string, string, int, error) {

	sub := nameRegex.FindStringSubmatch(src)

	if len(sub) != 6 {
		return "", "", 0, errors.New("Unable to parse docker --volume option: %s" + src)
	}

	// Set defaults
	pool := d.defPool
	name := sub[3]
	size := d.defSize

	// Pool overwrite
	if sub[2] != "" {
		pool = sub[2]
	}

	// Size overwrite
	if sub[5] != "" {
		var err error
		size, err = strconv.Atoi(sub[5])
		if err != nil {
			size = d.defSize
		}
	}

	return pool, name, size, nil
}

//-----------------------------------------------------------------------------
// imageExists
//-----------------------------------------------------------------------------

func (d *rbdDriver) imageExists(pool, name string) (bool, error) {

	// List RBD images
	out, err := exec.Command(d.cmd["rbd"], "ls", pool).Output()
	if err != nil {
		return false, errors.New("Unable to list images")
	}

	// Parse the output
	list := strings.Split(string(out), "\n")
	for _, item := range list {
		if item == name {
			return true, nil
		}
	}

	return false, nil
}

//-----------------------------------------------------------------------------
// createImage
//-----------------------------------------------------------------------------

func (d *rbdDriver) createImage(pool, name, fstype string, size int) error {

	// Create the image device
	err := exec.Command(
		d.cmd["rbd"], "create",
		"--pool", pool,
		"--size", strconv.Itoa(size),
		name,
	).Run()

	if err != nil {
		return errors.New("Unable to create the image device")
	}

	// Add image lock
	locker, err := d.lockImage(pool, name, lockID)
	if err != nil {
		return err
	}

	// Map the image to a kernel device
	device, err := d.mapImage(pool, name)
	if err != nil {
		defer d.unlockImage(pool, name, lockID, locker)
		return err
	}

	// Make the filesystem
	if err = d.makeFs(device, d.defFsType); err != nil {
		defer d.unmapImage(device)
		defer d.unlockImage(pool, name, lockID, locker)
		return err
	}

	// Unmap the image from kernel device
	if err = d.unmapImage(device); err != nil {
		return err
	}

	// Remove image lock
	if err = d.unlockImage(pool, name, lockID, locker); err != nil {
		return err
	}

	return nil
}

//-----------------------------------------------------------------------------
// lockImage
//-----------------------------------------------------------------------------

func (d *rbdDriver) lockImage(pool, name, lockID string) (string, error) {

	// Lock the image
	err := exec.Command(
		d.cmd["rbd"], "lock",
		"add", "--pool", pool,
		name, lockID,
	).Run()

	if err != nil {
		return "", errors.New("Unable to lock the image")
	}

	// List the locks
	out, err := exec.Command(
		d.cmd["rbd"], "lock", "list",
		"--pool", pool, name,
	).Output()

	if err != nil {
		return "", errors.New("Unable to list the image locks")
	}

	// Parse the locker ID
	lines := strings.Split(string(out), "\n")
	if len(lines) > 1 {
		for _, line := range lines[1:] {
			sub := lockRegex.FindStringSubmatch(line)
			if len(sub) == 2 {
				return sub[1], nil
			}
		}
	}

	return "", errors.New("Unable to parse locker ID")
}

//-----------------------------------------------------------------------------
// unlockImage
//-----------------------------------------------------------------------------

func (d *rbdDriver) unlockImage(pool, name, lockID, locker string) error {

	// Unlock the image
	err := exec.Command(
		d.cmd["rbd"], "lock", "remove",
		name, lockID, locker,
	).Run()

	if err != nil {
		return errors.New("Unable to unlock the image")
	}

	return nil
}

//-----------------------------------------------------------------------------
// mapImage
//-----------------------------------------------------------------------------

func (d *rbdDriver) mapImage(pool, name string) (string, error) {

	// Map the image to a kernel device
	out, err := exec.Command(
		d.cmd["rbd"], "map",
		"--pool", pool, name,
	).Output()

	if err != nil {
		return "", errors.New("Unable to map the image to a kernel device")
	}

	// Parse the device
	return strings.TrimSpace(string(out)), nil
}

//-----------------------------------------------------------------------------
// unmapImage
//-----------------------------------------------------------------------------

func (d *rbdDriver) unmapImage(device string) error {

	// Unmap the image from a kernel device
	err := exec.Command(
		d.cmd["rbd"], "unmap", device,
	).Run()

	if err != nil {
		return errors.New("Unable to unmap the image from " + device)
	}

	return nil
}

//-----------------------------------------------------------------------------
// makeFs
//-----------------------------------------------------------------------------

func (d *rbdDriver) makeFs(device, fsType string) error {

	// Search for mkfs
	mkfs, err := exec.LookPath("mkfs." + d.defFsType)
	if err != nil {
		return errors.New("Unable to find mkfs." + d.defFsType)
	}

	// Make the file system
	if err = exec.Command(mkfs, device).Run(); err != nil {
		return errors.New("Unable to make file system on " + device)
	}

	return nil
}

//-----------------------------------------------------------------------------
// mountDevice
//-----------------------------------------------------------------------------

func (d *rbdDriver) mountDevice(device, mountpoint, fsType string) error {

	// Mount the device
	err := exec.Command(
		d.cmd["mount"],
		"-t", fsType,
		device, mountpoint,
	).Run()

	if err != nil {
		return errors.New("Unable to mount " + device + "on " + mountpoint)
	}

	return nil
}

//-----------------------------------------------------------------------------
// unmountDevice
//-----------------------------------------------------------------------------

func (d *rbdDriver) unmountDevice(device string) error {

	// Unmount the device
	if err := exec.Command(d.cmd["umount"], device).Run(); err != nil {
		return errors.New("Unable to umount " + device)
	}

	return nil
}
