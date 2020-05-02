package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
)

const (
	vgConfigPath         = "/etc/docker/docker-gfs-plugin"
	gfsHome              = "/var/lib/docker-gfs-plugin"
	gfsVolumesConfigPath = "/var/lib/docker-gfs-plugin/gfsVolumesConfig.json"
	gfsCountConfigPath   = "/var/lib/docker-gfs-plugin/gfsCountConfig.json"
)

var (
	flVersion *bool
	flDebug   *bool
)

func init() {
	flVersion = flag.Bool("version", false, "Print version information and quit")
	flDebug = flag.Bool("debug", false, "Enable debug logging")
}

func main() {

	flag.Parse()

	if *flVersion {
		fmt.Fprint(os.Stdout, "docker gfs plugin version: 1.0.0\n")
		return
	}

	if *flDebug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	if _, err := exec.LookPath("mkfs.gfs2"); err != nil {
		logrus.Fatal("mkfs.gfs2 is not available, please install gfs2-utils to continue")
	}

	if _, err := os.Stat(gfsHome); err != nil {
		if !os.IsNotExist(err) {
			logrus.Fatal(err)
		}
		logrus.Debugf("Created home dir at %s", gfsHome)
		if err := os.MkdirAll(gfsHome, 0700); err != nil {
			logrus.Fatal(err)
		}
	}

	gfs, err := newDriver(gfsHome, vgConfigPath)
	if err != nil {
		logrus.Fatalf("Error initializing gfsDriver %v", err)
	}

	// Call loadFromDisk only if config file exists.
	if _, err := os.Stat(gfsVolumesConfigPath); err == nil {
		if err := loadFromDisk(gfs); err != nil {
			logrus.Fatal(err)
		}
	}

	h := volume.NewHandler(gfs)
	if err := h.ServeUnix("gfs", 0); err != nil {
		logrus.Fatal(err)
	}
}
