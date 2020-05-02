package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"
	"strconv"

	icmd "github.com/docker/docker/pkg/system"
)

func removeLogicalVolume(name, vgName string) ([]byte, error) {
	cmd := exec.Command("lvremove", "--force", fmt.Sprintf("%s/%s", vgName, name))
	if out, err := cmd.CombinedOutput(); err != nil {
		return out, err
	}
	return nil, nil
}

func getClusterName() (string, error) {
	vol := os.Getenv("CLUSTER_NAME")
	if len(vol) == 0 {
		return "mycluster", nil
	}
	return strings.TrimSpace(vol), nil
}
func getNodeCount() (string, error) {
	cnt := os.Getenv("NODE_COUNT")
	if len(cnt) == 0 {
		return "3", nil
	}
	i, err := strconv.ParseInt(strings.TrimSpace(cnt), 10, 64)
	if err != nil {
		return "3", err
	}
	return fmt.Sprintf("%d",i), nil
}
func getVolumegroupName() (string, error) {
	vgName := os.Getenv("VOLUME_GROUP")
	if len(vgName) == 0 {
		return "docker", nil
	}
	return strings.TrimSpace(vgName), nil
}

func getMountpoint(home, name string) string {
	return path.Join(home, name)
}

func saveToDisk(volumes map[string]*vol, count map[string]int) error {
	// Save volume store metadata.
	fhVolumes, err := os.Create(gfsVolumesConfigPath)
	if err != nil {
		return err
	}
	defer fhVolumes.Close()

	if err := json.NewEncoder(fhVolumes).Encode(&volumes); err != nil {
		return err
	}

	// Save count store metadata.
	fhCount, err := os.Create(gfsCountConfigPath)
	if err != nil {
		return err
	}
	defer fhCount.Close()

	return json.NewEncoder(fhCount).Encode(&count)
}

func loadFromDisk(l *gfsDriver) error {
	// Load volume store metadata
	jsonVolumes, err := os.Open(gfsVolumesConfigPath)
	if err != nil {
		return err
	}
	defer jsonVolumes.Close()

	if err := json.NewDecoder(jsonVolumes).Decode(&l.volumes); err != nil {
		return err
	}

	// Load count store metadata
	jsonCount, err := os.Open(gfsCountConfigPath)
	if err != nil {
		return err
	}
	defer jsonCount.Close()

	return json.NewDecoder(jsonCount).Decode(&l.count)
}

func lvdisplayGrep(vgName, lvName, keyword string) (bool, string, error) {
	var b2 bytes.Buffer

	cmd1 := exec.Command("lvdisplay", fmt.Sprintf("/dev/%s/%s", vgName, lvName))
	cmd2 := exec.Command("grep", keyword)

	r, w := io.Pipe()
	cmd1.Stdout = w
	cmd2.Stdin = r
	cmd2.Stdout = &b2

	if err := cmd1.Start(); err != nil {
		return false, "", err
	}
	if err := cmd2.Start(); err != nil {
		return false, "", err
	}
	if err := cmd1.Wait(); err != nil {
		return false, "", err
	}
	w.Close()
	if err := cmd2.Wait(); err != nil {
		exitCode, inErr := icmd.GetExitCode(err)
		if inErr != nil {
			return false, "", inErr
		}
		if exitCode != 1 {
			return false, "", err
		}
	}

	if b2.Len() != 0 {
		return true, b2.String(), nil
	}
	return false, "", nil
}

func isThinlyProvisioned(vgName, lvName string) (bool, string, error) {
	return lvdisplayGrep(vgName, lvName, "LV Pool")
}

func getVolumeCreationDateTime(vgName, lvName string) (time.Time, error) {
	_, creationDateTime, err := lvdisplayGrep(vgName, lvName, "LV Creation host")
	if err != nil {
		return time.Time{}, err
	}

	// creationDateTime is in the form "LV Creation host, time localhost, 2018-11-18 13:46:08 -0100"
	tokens := strings.Split(creationDateTime, ",")
	date := strings.TrimSpace(tokens[len(tokens)-1])
	return time.Parse("2006-01-02 15:04:05 -0700", date)
}

func keyFileExists(keyFile string) error {
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		return fmt.Errorf("key file does not exist: %s", keyFile)
	}
	return nil
}

func cryptsetupInstalled() error {
	if _, err := exec.LookPath("cryptsetup"); err != nil {
		return fmt.Errorf("'cryptsetup' executable not found")
	}
	return nil
}

func logicalDevice(vgName, lvName string) string {
	return fmt.Sprintf("/dev/%s/%s", vgName, lvName)
}

func luksDevice(lvName string) string {
	return fmt.Sprintf("/dev/mapper/%s", luksDeviceName(lvName))
}

func luksDeviceName(lvName string) string {
	return fmt.Sprintf("luks-%s", lvName)
}
