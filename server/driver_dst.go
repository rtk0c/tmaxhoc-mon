package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"
)

type DontStarveDriver struct {
	GameInstall string

	DataDir string
	Cluster string
	Shards  []string

	Use32bit  bool
	UseRlwrap bool

	LanOnly bool

	ShardUniqueMods bool
	UpdateMods      bool
}

// Logic adapted from https://github.com/rtk0c/scripts/blob/master/dstserv/dstserv_tmux.sh

// TODO only tested on linux

func (drv *DontStarveDriver) start(serv *ServiceUnit, ts *TmuxSession) error {
	var dstCwd, dstBin string
	if drv.Use32bit {
		dstCwd = path.Join(drv.GameInstall, "bin")
		dstBin = "./dontstarve_dedicated_server_nullrenderer"
	} else {
		dstCwd = path.Join(drv.GameInstall, "bin64")
		dstBin = "./dontstarve_dedicated_server_nullrenderer_x64"
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	os.Chdir(dstCwd)
	defer os.Chdir(cwd)

	dataDirPrefix := path.Dir(drv.DataDir)
	dataDirLast := path.Base(drv.DataDir)

	commonCommand := []string{
		dstBin,
		"-persistent_storage_root", dataDirPrefix,
		"-conf_dir", dataDirLast,
		"-ugc_directory", "../ugc_mods",
	}
	if drv.UseRlwrap {
		commonCommand = append([]string{
			"rlwrap",
			"-pGreen",
			"-C", "dont_starve_together",
			"-S", "> ",
			"-m", "-M", ".lua",
		}, commonCommand...)
	}
	if drv.LanOnly {
		commonCommand = append(commonCommand, "-lan")
	}
	makeCommand := func(args ...string) []string {
		newArgs := make([]string, len(commonCommand))
		copy(newArgs, commonCommand)
		newArgs = append(newArgs, args...)
		return newArgs
	}

	// Run one of the shard once in isolation, to avoid multiple processes racing to update the same files
	if !drv.ShardUniqueMods && drv.UpdateMods {
		args := []string{
			"-persistent_storage_root", dataDirPrefix,
			"-conf_dir", dataDirLast,
			"-ugc_directory", "../ugc_mods",
			"-cluster", drv.Cluster, "-shard", drv.Shards[0],
			"-only_update_server_mods",
		}
		cmd := exec.Command(dstBin, args...)
		err := cmd.Run()
		if err != nil {
			fmt.Printf("[WARN] [DST] updating mods failed: %s\n", err)
		}
	}

	for _, shard := range drv.Shards {
		cmdParts := makeCommand(
			"-cluster", drv.Cluster, "-shard", shard,
			"-console",
		)
		if !drv.ShardUniqueMods || (drv.ShardUniqueMods && drv.UpdateMods) {
			cmdParts = append(cmdParts, "-skip_update_server_mods")
		}
		_, err := ts.spawnProcess(serv.TmuxName, cmdParts...)
		if err != nil {
			fmt.Printf("[WARN] [DST] spawning shard %s failed: %s\n", shard, err)
		}
	}
}
