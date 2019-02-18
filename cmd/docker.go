// Copyright © 2016 Asteris, LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"errors"

	"github.com/docker/go-plugins-helpers/volume"
	"github.com/sirupsen/logrus"
	"github.com/soopsio/sshfs-go/docker"
	"github.com/soopsio/sshfs-go/fs"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// dockerCmd represents the docker command
var dockerCmd = &cobra.Command{
	Use:   "docker {mountpoint}",
	Short: "start the docker volume server at the specified root",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return errors.New("expected exactly one argument, a mountpoint")
		}

		if err := viper.BindPFlags(cmd.Flags()); err != nil {
			logrus.WithError(err).Fatal("could not bind flags")
		}

		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		driver, err := docker.New(docker.Config{
			Root:       viper.GetString("root"),
			MountPoint: args[0],
			SSHServer:  viper.GetString("address"),
			SSHConfig:  fs.NewConfig(viper.GetString("username"), viper.GetString("password")),
		})
		if err != nil {
			logrus.WithError(err).Fatal("driver init failed")
		}

		logrus.WithFields(logrus.Fields{
			"root":     args[0],
			"address":  viper.GetString("address"),
			"username": viper.GetString("username"),
			"socket":   viper.GetString("socket"),
		}).Info("starting plugin server")

		defer func() {
			for _, err := range driver.Stop() {
				logrus.WithError(err).Error("error stopping driver")
			}
		}()

		handler := volume.NewHandler(driver)
		logrus.WithField("socket", viper.GetString("socket")).Info("serving unix socket")
		err = handler.ServeUnix(viper.GetString("socket"), 0)
		if err != nil {
			logrus.WithError(err).Fatal("failed serving")
		}
	},
}

func init() {
	RootCmd.AddCommand(dockerCmd)

	dockerCmd.Flags().StringP("address", "a", "127.0.0.1:22", "ssh server address")
	dockerCmd.Flags().StringP("username", "u", "root", "ssh username")
	dockerCmd.Flags().StringP("password", "p", "", "ssh password")
	dockerCmd.Flags().StringP("root", "r", "/tmp", "remote root")
	dockerCmd.Flags().StringP("socket", "s", "/run/docker/plugins/ssh.sock", "socket address to communicate with docker")
}
