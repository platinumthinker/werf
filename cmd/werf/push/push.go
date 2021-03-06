package push

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/flant/werf/cmd/werf/common"
	"github.com/flant/werf/cmd/werf/common/docker_authorizer"
	"github.com/flant/werf/pkg/build"
	"github.com/flant/werf/pkg/docker"
	"github.com/flant/werf/pkg/lock"
	"github.com/flant/werf/pkg/logger"
	"github.com/flant/werf/pkg/project_tmp_dir"
	"github.com/flant/werf/pkg/ssh_agent"
	"github.com/flant/werf/pkg/true_git"
	"github.com/flant/werf/pkg/werf"
)

var CmdData struct {
	Repo       string
	WithStages bool

	PushUsername string
	PushPassword string
}

var CommonCmdData common.CmdData

func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push [IMAGE_NAME...]",
		Short: "Push built images into Docker registry",
		Long: common.GetLongCommandDescription(`Push built images into Docker registry.

Images will be tagged automatically with the names REPO/IMAGE_NAME:TAG. These tags will be deleted after push. See more info about images naming: https://flant.github.io/werf/reference/registry/image_naming.html.

If one or more IMAGE_NAME parameters specified, werf will push only these images from werf.yaml.`),
		DisableFlagsInUseLine: true,
		Annotations: map[string]string{
			common.CmdEnvAnno: common.EnvsDescription(common.WerfDockerConfig, common.WerfIgnoreCIDockerAutologin, common.WerfInsecureRegistry, common.WerfHome, common.WerfTmp),
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			err := runPush(args)
			if err != nil {
				return fmt.Errorf("push failed: %s", err)
			}
			return nil
		},
	}

	common.SetupDir(&CommonCmdData, cmd)
	common.SetupTmpDir(&CommonCmdData, cmd)
	common.SetupHomeDir(&CommonCmdData, cmd)
	common.SetupSSHKey(&CommonCmdData, cmd)

	cmd.Flags().StringVarP(&CmdData.Repo, "repo", "", "", "Docker repository name to push images to. CI_REGISTRY_IMAGE will be used by default if available.")
	cmd.Flags().BoolVarP(&CmdData.WithStages, "with-stages", "", false, "Push images with stages cache")

	cmd.Flags().StringVarP(&CmdData.PushUsername, "push-username", "", "", "Docker registry username to authorize push to the docker repo")
	cmd.Flags().StringVarP(&CmdData.PushPassword, "push-password", "", "", "Docker registry password to authorize push to the docker repo")
	cmd.Flags().StringVarP(&CmdData.PushUsername, "registry-username", "", "", "Docker registry username to authorize push to the docker repo")
	cmd.Flags().StringVarP(&CmdData.PushPassword, "registry-password", "", "", "Docker registry password to authorize push to the docker repo")

	common.SetupTag(&CommonCmdData, cmd)

	return cmd
}

func runPush(imagesToProcess []string) error {
	if err := werf.Init(*CommonCmdData.TmpDir, *CommonCmdData.HomeDir); err != nil {
		return fmt.Errorf("initialization error: %s", err)
	}

	if err := lock.Init(); err != nil {
		return err
	}

	if err := true_git.Init(); err != nil {
		return err
	}

	if err := docker.Init(docker_authorizer.GetHomeDockerConfigDir()); err != nil {
		return err
	}

	projectDir, err := common.GetProjectDir(&CommonCmdData)
	if err != nil {
		return fmt.Errorf("getting project dir failed: %s", err)
	}

	werfConfig, err := common.GetWerfConfig(projectDir)
	if err != nil {
		return fmt.Errorf("cannot parse werf config: %s", err)
	}

	projectName := werfConfig.Meta.Project

	projectBuildDir, err := common.GetProjectBuildDir(projectName)
	if err != nil {
		return fmt.Errorf("getting project build dir failed: %s", err)
	}

	projectTmpDir, err := project_tmp_dir.Get()
	if err != nil {
		return fmt.Errorf("getting project tmp dir failed: %s", err)
	}
	defer project_tmp_dir.Release(projectTmpDir)

	repo, err := common.GetRequiredRepoName(projectName, CmdData.Repo)
	if err != nil {
		return err
	}

	dockerAuthorizer, err := docker_authorizer.GetPushDockerAuthorizer(projectTmpDir, CmdData.PushUsername, CmdData.PushPassword, repo)
	if err != nil {
		return err
	}

	if err := ssh_agent.Init(*CommonCmdData.SSHKeys); err != nil {
		return fmt.Errorf("cannot initialize ssh agent: %s", err)
	}
	defer func() {
		err := ssh_agent.Terminate()
		if err != nil {
			logger.LogWarningF("WARNING: ssh agent termination failed: %s\n", err)
		}
	}()

	tagOpts, err := common.GetTagOptions(&CommonCmdData, projectDir)
	if err != nil {
		return err
	}

	pushOpts := build.PushOptions{TagOptions: tagOpts, WithStages: CmdData.WithStages}

	c := build.NewConveyor(werfConfig, imagesToProcess, projectDir, projectBuildDir, projectTmpDir, ssh_agent.SSHAuthSock, dockerAuthorizer)
	if err = c.Push(repo, pushOpts); err != nil {
		return err
	}

	return nil
}
