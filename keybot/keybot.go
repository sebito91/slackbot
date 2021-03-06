// Copyright 2015 Keybase, Inc. All rights reserved. Use of
// this source code is governed by the included BSD license.

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/keybase/slackbot"
	"github.com/keybase/slackbot/cli"
	"github.com/keybase/slackbot/launchd"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

type keybot struct{}

func (k *keybot) Run(bot slackbot.Bot, channel string, args []string) (string, error) {
	app := kingpin.New("keybot", "Job command parser for keybot")
	app.Terminate(nil)
	stringBuffer := new(bytes.Buffer)
	app.Writer(stringBuffer)

	build := app.Command("build", "Build things")

	cancel := app.Command("cancel", "Cancel")
	cancelLabel := cancel.Arg("label", "Launchd job label").Required().String()

	buildAndroid := build.Command("android", "Start an android build")
	buildIOS := build.Command("ios", "Start an ios build")
	buildIOSCientCommit := buildIOS.Flag("client-commit", "Build a specific client commit hash").String()
	buildIOSKbfsCommit := buildIOS.Flag("kbfs-commit", "Build a specific kbfs commit hash").String()

	release := app.Command("release", "Release things")
	releasePromote := release.Command("promote", "Promote a release to public")
	releaseToPromote := releasePromote.Arg("release-to-promote", "Promote a specific release to public immediately").String()
	releaseBroken := release.Command("broken", "Mark a release as broken")
	releaseBrokenVersion := releaseBroken.Arg("version", "Mark a release as broken").Required().String()

	smoketest := app.Command("smoketest", "Set the smoke testing status of a build")
	smoketestBuildA := smoketest.Flag("build-a", "The first of the two IDs comprising the new build").Required().String()
	smoketestPlatform := smoketest.Flag("platform", "The build's platform (darwin, linux, windows)").Required().String()
	smoketestEnable := smoketest.Flag("enable", "Whether smoketesting should be enabled").Required().Bool()
	smoketestMaxTesters := smoketest.Flag("max-testers", "Max number of testers for this build").Required().Int()

	dumplogCmd := app.Command("dumplog", "Show the log file")
	dumplogCommandLabel := dumplogCmd.Arg("label", "Launchd job label").Required().String()

	upgrade := app.Command("upgrade", "Upgrade package")
	upgradePackageName := upgrade.Arg("name", "Package name (yarn, go, fastlane, etc)").Required().String()

	cmd, usage, cmdErr := cli.Parse(app, args, stringBuffer)
	if usage != "" || cmdErr != nil {
		return usage, cmdErr
	}

	if bot.Config().DryRun() {
		return fmt.Sprintf("I would have run: `%#v`", cmd), nil
	}

	home := os.Getenv("HOME")
	shims := filepath.Join(home, ".rbenv/shims")
	path := "/sbin:/usr/sbin:/bin:/usr/bin:/usr/local/bin:" + shims
	env := launchd.NewEnv(home, path)
	switch cmd {
	case cancel.FullCommand():
		return launchd.Stop(*cancelLabel)

	case buildAndroid.FullCommand():

		script := launchd.Script{
			Label:      "keybase.build.android",
			Path:       "github.com/keybase/client/packaging/android/build_and_publish.sh",
			BucketName: "prerelease.keybase.io",
			EnvVars: []launchd.EnvVar{
				launchd.EnvVar{Key: "ANDROID_HOME", Value: "/usr/local/opt/android-sdk"},
			},
		}
		env.GoPath = env.PathFromHome("go-android") // Custom go path for Android so we don't conflict
		return runScript(bot, channel, env, script)

	case buildIOS.FullCommand():
		script := launchd.Script{
			Label:      "keybase.build.ios",
			Path:       "github.com/keybase/client/packaging/ios/build_and_publish.sh",
			BucketName: "prerelease.keybase.io",
			EnvVars: []launchd.EnvVar{
				launchd.EnvVar{Key: "CLIENT_COMMIT", Value: *buildIOSCientCommit},
				launchd.EnvVar{Key: "KBFS_COMMIT", Value: *buildIOSKbfsCommit},
			},
		}
		env.GoPath = env.PathFromHome("go-ios") // Custom go path for iOS so we don't conflict
		return runScript(bot, channel, env, script)

	case releasePromote.FullCommand():
		script := launchd.Script{
			Label:      "keybase.release.promote",
			Path:       "github.com/keybase/slackbot/scripts/release.promote.sh",
			BucketName: "prerelease.keybase.io",
			Platform:   "darwin",
			EnvVars: []launchd.EnvVar{
				launchd.EnvVar{Key: "RELEASE_TO_PROMOTE", Value: *releaseToPromote},
			},
		}
		return runScript(bot, channel, env, script)

	case dumplogCmd.FullCommand():
		readPath, err := env.LogPathForLaunchdLabel(*dumplogCommandLabel)
		if err != nil {
			return "", err
		}
		script := launchd.Script{
			Label:      "keybase.dumplog",
			Path:       "github.com/keybase/slackbot/scripts/dumplog.sh",
			BucketName: "prerelease.keybase.io",
			EnvVars: []launchd.EnvVar{
				launchd.EnvVar{Key: "READ_PATH", Value: readPath},
				launchd.EnvVar{Key: "NOLOG", Value: boolToEnvString(true)},
			},
		}
		return runScript(bot, channel, env, script)

	case releaseBroken.FullCommand():
		script := launchd.Script{
			Label:      "keybase.release.broken",
			Path:       "github.com/keybase/slackbot/scripts/release.broken.sh",
			BucketName: "prerelease.keybase.io",
			Platform:   "darwin",
			EnvVars: []launchd.EnvVar{
				launchd.EnvVar{Key: "BROKEN_RELEASE", Value: *releaseBrokenVersion},
			},
		}
		return runScript(bot, channel, env, script)

	case smoketest.FullCommand():
		script := launchd.Script{
			Label:      "keybase.smoketest",
			Path:       "github.com/keybase/slackbot/scripts/smoketest.sh",
			BucketName: "prerelease.keybase.io",
			Platform:   *smoketestPlatform,
			EnvVars: []launchd.EnvVar{
				launchd.EnvVar{Key: "SMOKETEST_BUILD_A", Value: *smoketestBuildA},
				launchd.EnvVar{Key: "SMOKETEST_MAX_TESTERS", Value: strconv.Itoa(*smoketestMaxTesters)},
				launchd.EnvVar{Key: "SMOKETEST_ENABLE", Value: boolToString(*smoketestEnable)},
			},
		}
		return runScript(bot, channel, env, script)

	case upgrade.FullCommand():
		script := launchd.Script{
			Label: "keybase.update",
			Path:  "github.com/keybase/slackbot/scripts/update.sh",
			EnvVars: []launchd.EnvVar{
				launchd.EnvVar{Key: "NAME", Value: *upgradePackageName},
			},
		}
		return runScript(bot, channel, env, script)
	}
	return cmd, nil
}

func (k *keybot) Help(bot slackbot.Bot) string {
	out, err := k.Run(bot, "", nil)
	if err != nil {
		return fmt.Sprintf("Error getting help: %s", err)
	}
	return out
}
