{% if include.header %}
{% assign header = include.header %}
{% else %}
{% assign header = "###" %}
{% endif %}
Tag built images from werf.yaml.

Stages cache should exists for images to be tagged. I.e. images should be built with build command 
before tagging. Docker images names are constructed from parameters as REPO/IMAGE_NAME:TAG. See 
more info about images naming: https://flant.github.io/werf/reference/registry/image_naming.html.

If one or more IMAGE_NAME parameters specified, werf will tag only these images from werf.yaml. 

{{ header }} Syntax

```bash
werf tag [IMAGE_NAME...] [options]
```

{{ header }} Options

```bash
      --dir='':
            Change to the specified directory to find werf.yaml config
  -h, --help=false:
            help for tag
      --home-dir='':
            Use specified dir to store werf cache files and dirs (use ~/.werf by default)
      --repo='':
            Docker repository name to tag images for. CI_REGISTRY_IMAGE will be used by default if 
            available.
      --ssh-key=[]:
            Enable only specified ssh keys (use system ssh-agent by default)
      --tag=[]:
            Add tag (can be used one or more times)
      --tag-branch=false:
            Tag by git branch
      --tag-build-id=false:
            Tag by CI build id
      --tag-ci=false:
            Tag by CI branch and tag
      --tag-commit=false:
            Tag by git commit
      --tmp-dir='':
            Use specified dir to store tmp files and dirs (use system tmp dir by default)
```

