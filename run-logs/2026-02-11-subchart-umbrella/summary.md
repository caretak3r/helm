# Summary

Created a new umbrella chart at `pkg/action/testdata/charts/subchart-umbrella` with 4 subcharts (`db`, `cache`, `api`, `frontend`) and explicit dependency ordering (`db` → `cache` → `api` → `frontend`), with minimal `ConfigMap` and `Deployment` templates to make resources visible in k9s.

Deploy example (local Helm binary):

```
/Users/rohit/Documents/helm/bin/helm upgrade --install demo /Users/rohit/Documents/helm/pkg/action/testdata/charts/subchart-umbrella --wait=ordered -n default
```
