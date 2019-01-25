package build

import (
	"fmt"

	imagePkg "github.com/flant/werf/pkg/image"
	"github.com/flant/werf/pkg/lock"
	"github.com/flant/werf/pkg/logger"
)

func NewBuildPhase(opts BuildOptions) *BuildPhase {
	return &BuildPhase{opts}
}

type BuildOptions struct {
	ImageBuildOptions imagePkg.BuildOptions
}

type BuildPhase struct {
	BuildOptions
}

func (p *BuildPhase) Run(c *Conveyor) (err error) {
	return p.run(c)
}

func (p *BuildPhase) run(c *Conveyor) (err error) {
	if debug() {
		fmt.Printf("BuildPhase.Run\n")
	}

	for _, image := range c.imagesInOrder {
		logger.LogServiceProcess(fmt.Sprintf("Build %s images", image.name), " ", func() error {
			if err = p.runImage(image, c); err != nil {
				return err
			}

			return nil
		})
	}

	return nil
}

func (p *BuildPhase) runImage(image *Image, c *Conveyor) (err error) {
	if debug() {
		fmt.Printf("  image: '%s'\n", image.GetName())
	}

	var acquiredLocks []string

	unlockLock := func() {
		var lockName string
		lockName, acquiredLocks = acquiredLocks[0], acquiredLocks[1:]
		lock.Unlock(lockName)
	}

	unlockLocks := func() {
		for len(acquiredLocks) > 0 {
			unlockLock()
		}
	}

	defer unlockLocks()

	// lock
	for _, stage := range image.GetStages() {
		img := stage.GetImage()
		if img.IsExists() {
			continue
		}

		imageLockName := fmt.Sprintf("%s.image.%s", c.projectName(), img.Name())
		err = lock.Lock(imageLockName, lock.LockOptions{})
		if err != nil {
			return fmt.Errorf("failed to lock %s: %s", imageLockName, err)
		}

		acquiredLocks = append(acquiredLocks, imageLockName)

		if err = img.SyncDockerState(); err != nil {
			return err
		}
	}

	// build
	for _, s := range image.GetStages() {
		img := s.GetImage()
		msg := fmt.Sprintf("%s", s.Name())

		if img.IsExists() {
			logger.LogServiceState(msg, "[USING CACHE]")

			logger.LogInfoF("       id: %+v\n", img.Inspect().ID)
			logger.LogInfoF("    image: %+v\n", img.Name())
			logger.LogInfoF("     size: %+v\n", img.Inspect().Size)
			logger.LogInfoF("  created: %+v\n\n", img.Inspect().Created)

			continue
		}

		logger.LogProcess(msg, "[BUILDING]", func() error {
			if debug() {
				fmt.Printf("    %s\n", s.Name())
			}

			if err = s.PreRunHook(c); err != nil {
				return fmt.Errorf("stage '%s' preRunHook failed: %s", s.Name(), err)
			}

			if err = img.Build(p.ImageBuildOptions); err != nil {
				return fmt.Errorf("failed to build %s: %s", img.Name(), err)
			}

			err = img.SaveInCache()
			if err != nil {
				return fmt.Errorf("failed to save in cache image %s: %s", img.Name(), err)
			}

			logger.LogInfoF("    image: %+v\n", img.Name())
			logger.LogInfoF("    id: %+v\n", img.Inspect().ID)
			logger.LogInfoF("    size: %+v\n", img.Inspect().Size)
			logger.LogInfoF("    created: %+v\n", img.Inspect().Created)

			return nil
		})

		unlockLock()
	}

	return
}
