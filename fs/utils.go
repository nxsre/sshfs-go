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

package fs

import (
	"golang.org/x/crypto/ssh"
	"log"
	"net"
)

// NewConfig creates a new config
func NewConfig(user, password string, privateKeyPath string) *ssh.ClientConfig {
	auth := []ssh.AuthMethod{
		ssh.Password(password),
	}

	publicKey, err := PublicKeyFile(privateKeyPath)
	if err == nil {
		auth = append(auth, publicKey)
	} else {
		log.Println(err)
	}

	return &ssh.ClientConfig{
		User: user,
		Auth: auth,
		Config: ssh.Config{
			Ciphers: []string{"aes128-ctr", "aes192-ctr", "aes256-ctr", "aes128-gcm@openssh.com", "arcfour256", "arcfour128", "aes128-cbc", "3des-cbc", "aes192-cbc", "aes256-cbc"},
		},
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return nil
		},
	}
}
