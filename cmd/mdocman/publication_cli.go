package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

func runPublicationCLI(s *server, args []string, out io.Writer) error {
	unlock, err := s.lockPublication()
	if err != nil {
		return err
	}
	defer unlock()
	scoped, root, err := s.publicationScope()
	if err != nil {
		return err
	}
	config, err := readPublicationConfig(root)
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "help" {
		fmt.Fprintln(out, "mdoc publish config [config.json]\nmdoc publish theme install\nmdoc publish build\nmdoc publish deploy\n只发布当前知识库中 publish: true 且非私密、非回收站、非草稿的文档。")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	switch args[0] {
	case "config":
		if len(args) > 2 {
			return errors.New("用法：mdoc publish config [config.json]")
		}
		if len(args) == 2 {
			data, err := os.ReadFile(args[1])
			if err != nil {
				return err
			}
			if err = json.Unmarshal(data, &config); err != nil {
				return err
			}
			if err = validatePublicationConfig(config); err != nil {
				return err
			}
			if err = writePublicationJSON(filepath.Join(root, "config.json"), config); err != nil {
				return err
			}
		}
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(config)
	case "theme":
		if len(args) != 2 || args[1] != "install" {
			return errors.New("用法：mdoc publish theme install")
		}
		log, err := scoped.installPublicationTheme(ctx, config)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, log)
		return nil
	case "build", "deploy":
		if target := os.Getenv("DEPLOY_TARGET"); target != "" {
			config.DeployTarget = target
		}
		if len(args) != 1 {
			return errors.New("用法：mdoc publish build 或 mdoc publish deploy")
		}
		result, err := scoped.runPublication(ctx, root, config, args[0] == "deploy")
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(result)
	default:
		return errors.New("未知发布命令；运行 mdoc publish help")
	}
}
