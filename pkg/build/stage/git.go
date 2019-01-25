package stage

import (
	"github.com/flant/werf/pkg/image"
	"github.com/flant/werf/pkg/logger"
)

func newGitStage(name StageName, baseStageOptions *NewBaseStageOptions) *GitStage {
	s := &GitStage{}
	s.BaseStage = newBaseStage(name, baseStageOptions)
	return s
}

type GitStage struct {
	*BaseStage
}

func (s *GitStage) IsEmpty(_ Conveyor, prevBuiltImage image.ImageInterface) (bool, error) {
	return len(s.gitPaths) == 0, nil
}

func (s *GitStage) ShouldBeReset(builtImage image.ImageInterface) (bool, error) {
	for _, gitPath := range s.gitPaths {
		commit := gitPath.GetGitCommitFromImageLabels(builtImage)
		if exist, err := gitPath.GitRepo().IsCommitExists(commit); err != nil {
			return false, err
		} else if !exist {
			return true, nil
		}
	}

	return false, nil
}

func (s *GitStage) AfterImageSyncDockerStateHook(c Conveyor) error {
	if !s.image.IsExists() {
		stageName := c.GetBuildingGitStage(s.imageName)
		if stageName == "" {
			c.SetBuildingGitStage(s.imageName, s.Name())

			logger.LogInfoF("Git files will be actualized on the stage %s\n", s.Name())
		}
	}

	return nil
}

func (s *GitStage) PrepareImage(c Conveyor, prevBuiltImage, image image.ImageInterface) error {
	if err := s.BaseStage.PrepareImage(c, prevBuiltImage, image); err != nil {
		return err
	}

	return nil
}
