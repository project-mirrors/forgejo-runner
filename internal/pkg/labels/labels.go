// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package labels

import (
	"fmt"
	"strings"
)

const (
	SchemeHost = "host"

	SchemeDocker = "docker"
	ArgDocker    = "//node:22-bookworm"

	SchemeLXC = "lxc"
	ArgLXC    = "//debian:bookworm"
)

type Label struct {
	Name   string
	Schema string
	Arg    string
}

func Parse(str string) (*Label, error) {
	splits := strings.SplitN(str, ":", 3)
	label := &Label{
		Name:   splits[0],
		Schema: "docker",
	}

	if strings.TrimSpace(label.Name) != label.Name {
		return nil, fmt.Errorf("invalid label %q: starting or ending with a space is invalid", label.Name)
	}

	if len(splits) >= 2 {
		label.Schema = splits[1]
		if label.Schema != SchemeHost && label.Schema != SchemeDocker && label.Schema != SchemeLXC {
			return nil, fmt.Errorf("unsupported schema: %s", label.Schema)
		}
	}

	if len(splits) >= 3 {
		if label.Schema == SchemeHost {
			return nil, fmt.Errorf("schema: %s does not have arguments", label.Schema)
		}

		label.Arg = splits[2]
	}
	if label.Arg == "" {
		switch label.Schema {
		case SchemeDocker:
			label.Arg = ArgDocker
		case SchemeLXC:
			label.Arg = ArgLXC
		}
	}

	return label, nil
}

type Labels []*Label

func (l Labels) RequireDocker() bool {
	for _, label := range l {
		if label.Schema == SchemeDocker {
			return true
		}
	}
	return false
}

func (l Labels) PickPlatform(runsOn []string) string {
	platforms := make(map[string]string, len(l))
	for _, label := range l {
		switch label.Schema {
		case SchemeDocker:
			// "//" will be ignored
			platforms[label.Name] = strings.TrimPrefix(label.Arg, "//")
		case SchemeHost:
			platforms[label.Name] = "-self-hosted"
		case SchemeLXC:
			platforms[label.Name] = "lxc:" + strings.TrimPrefix(label.Arg, "//")
		default:
			// It should not happen, because Parse has checked it.
			continue
		}
	}
	for _, v := range runsOn {
		if v, ok := platforms[v]; ok {
			return v
		}
	}

	return strings.TrimPrefix(ArgDocker, "//")
}

func (l Labels) Names() []string {
	names := make([]string, 0, len(l))
	for _, label := range l {
		names = append(names, label.Name)
	}
	return names
}

func (l Labels) ToStrings() []string {
	ls := make([]string, 0, len(l))
	for _, label := range l {
		lbl := label.Name
		if label.Schema != "" {
			lbl += ":" + label.Schema
			if label.Arg != "" {
				lbl += ":" + label.Arg
			}
		}
		ls = append(ls, lbl)
	}
	return ls
}
