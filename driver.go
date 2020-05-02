package main

import (
	"fmt"
	"log/syslog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/docker/docker/pkg/mount"
	"github.com/docker/go-plugins-helpers/volume"
	"github.com/sirupsen/logrus"
	units "github.com/docker/go-units"
)

type gfsDriver struct {
	home     string
	vgConfig string
	volumes  map[string]*vol
	count    map[string]int
	mu       sync.RWMutex
	logger   *syslog.Writer
}

type vol struct {
	Name       string `json:"name"`
	VgName     string `json:"vgname"`
	MountPoint string `json:"mountpoint"`
	Type       string `json:"type"`
	Source     string `json:"source"`
	KeyFile    string `json:"keyfile"`
}

func newDriver(home, vgConfig string) (*gfsDriver, error) {
	logger, err := syslog.New(syslog.LOG_ERR, "docker-gfs-plugin")
	if err != nil {
		return nil, err
	}

	return &gfsDriver{
		home:     home,
		vgConfig: vgConfig,
		volumes:  make(map[string]*vol),
		count:    make(map[string]int),
		logger:   logger,
	}, nil
}

func (l *gfsDriver) Create(req *volume.CreateRequest) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, err := os.Stat(gfsVolumesConfigPath); err == nil {
		if err := loadFromDisk(l); err != nil {
			logrus.Fatal(err)
		}
	}
	if _, exists := l.volumes[req.Name]; exists {
		return nil
	}

	vgName, err := getVolumegroupName()
	if err != nil {
		return err
	}
	if vgNameOpt, ok := req.Options["vg"]; ok && vgNameOpt != "" {
		vgName = vgNameOpt
	}
	nCount, err := getNodeCount()
	if err != nil {
		return err
	}
	if nCountOpt, ok := req.Options["node_count"]; ok && nCountOpt != "" {
		nCount = nCountOpt
	}
	cName, err := getClusterName()
	if err != nil {
		return err
	}
	if cNameOpt, ok := req.Options["cluster_name"]; ok && cNameOpt != "" {
		cName = cNameOpt
	}

	keyFile, ok := req.Options["keyfile"]
	hasKeyFile := ok && keyFile != ""
	if hasKeyFile {
		if err := keyFileExists(keyFile); err != nil {
			return err
		}
		if err := cryptsetupInstalled(); err != nil {
			return err
		}
	}

	cmdArgs := []string{"-asy", "--name", req.Name}
	s, ok := req.Options["size"]
	hasSize := ok && s != ""

	if hasSize {
		sizeInBytes, err := units.FromHumanSize(s)
		if err != nil {
			return err
		}
		if sizeInBytes < 16000000 {
			return fmt.Errorf("Error creating LVM volume, minimum expected size is 16M")
		}
	}

	if !hasSize {
		return fmt.Errorf("Please specify a size with --opt size=")
	}

	cmdArgs = append(cmdArgs, "--size", s)
	cmdArgs = append(cmdArgs, vgName)
	cmd := exec.Command("lvcreate", cmdArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		l.logger.Err(fmt.Sprintf("Create: lvcreate error: %s output %s", err, string(out)))
		return fmt.Errorf("Error creating volume")
	}

	defer func() {
		if err != nil {
			removeLogicalVolume(req.Name, vgName)
		}
	}()

	device := logicalDevice(vgName, req.Name)

	if hasKeyFile {
		cmd = exec.Command("cryptsetup", "-q", "-d", keyFile, "luksFormat", device)
		if out, err := cmd.CombinedOutput(); err != nil {
			l.logger.Err(fmt.Sprintf("Create: cryptsetup error: %s output %s", err, string(out)))
			return fmt.Errorf("Error encrypting volume")
		}

		if out, err := luksOpen(vgName, req.Name, keyFile); err != nil {
			l.logger.Err(fmt.Sprintf("Create: cryptsetup error: %s output %s", err, string(out)))
			return fmt.Errorf("Error opening encrypted volume")
		}

		defer func() {
			if err != nil {
				if out, err := luksClose(req.Name); err != nil {
					l.logger.Err(fmt.Sprintf("Create: cryptsetup error: %s output %s", err, string(out)))
				}
			}
		}()

		device = luksDevice(req.Name)
	}

	cmd = exec.Command("mkfs.gfs2", "-O", "-j", nCount, "-t", cName+":"+req.Name, device)
	if out, err := cmd.CombinedOutput(); err != nil {
		l.logger.Err(fmt.Sprintf("Create: mkfs.gfs2 error: %s output %s", err, string(out)))
		return fmt.Errorf("Error partitioning volume")
	}

	if hasKeyFile {
		if out, err := luksClose(req.Name); err != nil {
			l.logger.Err(fmt.Sprintf("Create: cryptsetup error: %s output %s", err, string(out)))
			return fmt.Errorf("Error closing encrypted volume")
		}
	}

	mp := getMountpoint(l.home, req.Name)
	err = os.MkdirAll(mp, 0700)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(mp)
		}
	}()

	v := &vol{Name: req.Name, VgName: vgName, MountPoint: mp}
	if hasKeyFile {
		v.KeyFile = keyFile
	}
	l.volumes[v.Name] = v
	l.count[v.Name] = 0
	err = saveToDisk(l.volumes, l.count)
	if err != nil {
		return err
	}
	return nil
}

func (l *gfsDriver) List() (*volume.ListResponse, error) {
	l.logger.Info("List, accessing the lock")
	l.mu.RLock()
	defer l.mu.RUnlock()
	l.logger.Info("List, reading the datafiles")
	if _, err := os.Stat(gfsVolumesConfigPath); err == nil {
		if err := loadFromDisk(l); err != nil {
			logrus.Fatal(err)
		}
	}
	l.logger.Info("List, Create the reply array")
	var ls []*volume.Volume
	for _, vol := range l.volumes {
		v := &volume.Volume{
			Name:       vol.Name,
			Mountpoint: vol.MountPoint,
			// TO-DO: Find the significance of status field, and add that to volume.Volume
		}
		ls = append(ls, v)
	}
	l.logger.Info("List, done")
	return &volume.ListResponse{Volumes: ls}, nil
}

func (l *gfsDriver) Get(req *volume.GetRequest) (*volume.GetResponse, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	vgName, err := getVolumegroupName()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(gfsVolumesConfigPath); err == nil {
		if err := loadFromDisk(l); err != nil {
			logrus.Fatal(err)
		}
	}

	v, exists := l.volumes[req.Name]
	if !exists {
		return &volume.GetResponse{}, fmt.Errorf("No such volume")
	}

	if v.VgName == "" {
		v.VgName = vgName
	}

	createdAt, err := getVolumeCreationDateTime(v.VgName, v.Name)
	if err != nil {
		return nil, err
	}

	var res volume.GetResponse
	res.Volume = &volume.Volume{
		Name:       v.Name,
		Mountpoint: v.MountPoint,
		CreatedAt:  fmt.Sprintf(createdAt.Format(time.RFC3339)),
	}
	return &res, nil
}

func (l *gfsDriver) Remove(req *volume.RemoveRequest) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, err := os.Stat(gfsVolumesConfigPath); err == nil {
		if err := loadFromDisk(l); err != nil {
			logrus.Fatal(err)
		}
	}

	vgName, err := getVolumegroupName()
	if err != nil {
		return err
	}
	vol, exists := l.volumes[req.Name]
	if !exists {
		return fmt.Errorf("Unknown volume %s", req.Name)
	}
	if vol.VgName == "" {
		vol.VgName = vgName
	}

	isOrigin := func() bool {
		for _, vol := range l.volumes {
			if vol.Name == req.Name {
				continue
			}
			if vol.Type == "Snapshot" && vol.Source == req.Name {
				return true
			}
		}
		return false
	}()

	if isOrigin {
		return fmt.Errorf("Error removing volume, all snapshot destinations must be removed before removing the original volume")
	}

	if err := os.RemoveAll(getMountpoint(l.home, req.Name)); err != nil {
		return err
	}

	if out, err := removeLogicalVolume(req.Name, vol.VgName); err != nil {
		l.logger.Err(fmt.Sprintf("Remove: removeLogicalVolume error %s output %s", err, string(out)))
		return fmt.Errorf("error removing volume")
	}

	delete(l.count, req.Name)
	delete(l.volumes, req.Name)
	if err := saveToDisk(l.volumes, l.count); err != nil {
		return err
	}
	return nil
}

func (l *gfsDriver) Path(req *volume.PathRequest) (*volume.PathResponse, error) {
	return &volume.PathResponse{Mountpoint: getMountpoint(l.home, req.Name)}, nil
}

func (l *gfsDriver) Mount(req *volume.MountRequest) (*volume.MountResponse, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, err := os.Stat(gfsVolumesConfigPath); err == nil {
		if err := loadFromDisk(l); err != nil {
			logrus.Fatal(err)
		}
	}
	vgName, err := getVolumegroupName()
	if err != nil {
		return &volume.MountResponse{}, err
	}
	vol, exists := l.volumes[req.Name]
	if !exists {
		return &volume.MountResponse{}, fmt.Errorf("Unknown volume %s", req.Name)
	}
	if vol.VgName == "" {
		vol.VgName = vgName
	}
	cmd := exec.Command("/usr/sbin/vgchange", "-asy", "docker")
	if out, err := cmd.CombinedOutput(); err != nil {
		l.logger.Err(fmt.Sprintf("vgchange:  error: %s output %s", err, string(out)))
	}


	isSnap, keyFile := func() (bool, string) {
		if v, ok := l.volumes[req.Name]; ok {
			if v.Type == "Snapshot" {
				return true, v.KeyFile
			}
			return false, v.KeyFile
		}
		return false, ""
	}()

	device := logicalDevice(vol.VgName, req.Name)

	if keyFile != "" {
		if err := keyFileExists(keyFile); err != nil {
			l.logger.Err(fmt.Sprintf("Mount: %s", err))
			return &volume.MountResponse{}, err
		}
		if err := cryptsetupInstalled(); err != nil {
			l.logger.Err(fmt.Sprintf("Mount: %s", err))
			return &volume.MountResponse{}, err
		}
		if out, err := luksOpen(vol.VgName, req.Name, keyFile); err != nil {
			l.logger.Err(fmt.Sprintf("Mount: cryptsetup error: %s output %s", err, string(out)))
			return &volume.MountResponse{}, fmt.Errorf("Error opening encrypted volume")
		}
		defer func() {
			if err != nil {
				if out, err := luksClose(req.Name); err != nil {
					l.logger.Err(fmt.Sprintf("Mount: cryptsetup error: %s output %s", err, string(out)))
				}
			}
		}()
		device = luksDevice(req.Name)
	}

	mountArgs := []string{device, getMountpoint(l.home, req.Name)}
	if isSnap {
		mountArgs = append([]string{"-o", "nouuid"}, mountArgs...)
	}
	cmd = exec.Command("mount", mountArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		l.logger.Err(fmt.Sprintf("Mount: mount error: %s output %s", err, string(out)))
		return &volume.MountResponse{}, fmt.Errorf("Error mouting volume")
	}
	l.count[req.Name]++
	if err := saveToDisk(l.volumes, l.count); err != nil {
		return &volume.MountResponse{}, err
	}
	return &volume.MountResponse{Mountpoint: getMountpoint(l.home, req.Name)}, nil
}

func (l *gfsDriver) Unmount(req *volume.UnmountRequest) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := os.Stat(gfsVolumesConfigPath); err == nil {
		if err := loadFromDisk(l); err != nil {
			logrus.Fatal(err)
		}
	}
	if l.count[req.Name] == 1 {
		mp := getMountpoint(l.home, req.Name)
		isVolMounted, err := mount.Mounted(mp)
		if err != nil {
			l.logger.Err(fmt.Sprintf("Unmount: %s", err))
			return fmt.Errorf("error unmounting volume")
		}
		if isVolMounted {
			cmd := exec.Command("umount", mp)
			if out, err := cmd.CombinedOutput(); err != nil {
				l.logger.Err(fmt.Sprintf("Unmount: unmount error: %s output %s", err, string(out)))
				return fmt.Errorf("error unmounting volume")
			}
			if v, ok := l.volumes[req.Name]; ok && v.KeyFile != "" {
				if err := cryptsetupInstalled(); err != nil {
					l.logger.Err(fmt.Sprintf("Unmount: %s", err))
					return err
				}
				if out, err := luksClose(req.Name); err != nil {
					l.logger.Err(fmt.Sprintf("Unmount: cryptsetup error: %s output %s", err, string(out)))
					return fmt.Errorf("Error closing encrypted volume")
				}
			}
		}
	}
	l.count[req.Name]--
	if err := saveToDisk(l.volumes, l.count); err != nil {
		return err
	}
	return nil
}

func (l *gfsDriver) Capabilities() *volume.CapabilitiesResponse {
	var res volume.CapabilitiesResponse
	res.Capabilities = volume.Capability{Scope: "global"}
	return &res
}

func luksOpen(vgName, volName, keyFile string) ([]byte, error) {
	cmd := exec.Command("cryptsetup", "-d", keyFile, "luksOpen", logicalDevice(vgName, volName), luksDeviceName(volName))
	if out, err := cmd.CombinedOutput(); err != nil {
		return out, err
	}
	return nil, nil
}

func luksClose(volName string) ([]byte, error) {
	cmd := exec.Command("cryptsetup", "luksClose", luksDeviceName(volName))
	if out, err := cmd.CombinedOutput(); err != nil {
		return out, err
	}
	return nil, nil
}
