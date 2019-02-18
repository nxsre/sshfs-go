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
	"github.com/sirupsen/logrus"
	"github.com/soopsio/sshfs/fs"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"os"
	"os/signal"
	"syscall"
)

// mountCmd represents the mount command
var mountCmd = &cobra.Command{
	Use:   "mount {mountpoint}",
	Short: "mount a SSHFS at the specified mountpoint",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return errors.New("expected exactly one argument")
		}

		if err := viper.BindPFlags(cmd.Flags()); err != nil {
			logrus.WithError(err).Fatal("could not bind flags")
		}

		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		config := fs.NewConfig(viper.GetString("username"), viper.GetString("password"))
		logrus.WithField("address", viper.GetString("address")).Info("creating FUSE client for SSH Server")

		fs, err := fs.New(config, args[0], viper.GetString("address"), viper.GetString("root"))
		if err != nil {
			logrus.WithError(err).Fatal("error creatinging fs")
		}

		// handle interrupt
		go func() {
			c := make(chan os.Signal, 1)
			signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

			<-c
			logrus.Info("stopping")
			err := fs.Unmount()
			if err != nil {
				logrus.WithError(err).Fatal("could not unmount cleanly")
			}
		}()

		err = fs.Mount()
		if err != nil {
			logrus.WithError(err).Fatal("could not continue")
		}
	},
}

func init() {
	RootCmd.AddCommand(mountCmd)

	mountCmd.Flags().StringP("address", "a", "127.0.0.1:22", "ssh server address")
	mountCmd.Flags().StringP("username", "u", "root", "ssh username")
	mountCmd.Flags().StringP("password", "p", "", "ssh password")
	mountCmd.Flags().StringP("root", "r", "/opt", "ssh root")
}
