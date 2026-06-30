[![REUSE status](https://api.reuse.software/badge/github.com/open-component-model/ocm-integrationtest)](https://api.reuse.software/info/github.com/open-component-model/ocm-integrationtest) [![OCM Integration Tests](https://github.com/open-component-model/ocm-integrationtest/actions/workflows/integrationtest.yaml/badge.svg?branch=main)](https://open-component-model.github.io/ocm-integrationtest/report.html)

# Open-Component-Model Integration Test

## About this project

This repository runs the OCM integration tests. It installs a local OCI registry, performs various OCM commands and checks for the expected results.

## Requirements and Setup

This project uses Python 3.9+ to run the tests against the [OCM CLI](https://github.com/open-component-model/ocm). It is targeted to be executed in a Github action workflow. You can also run the tests locally:

* Install Python 3.9+
* Create a virtual environment, e.g `python -m venv <path-to-your-env>/ocmtest`
* Install pip: `python -m pip install --upgrade pip`
* Activate environment: `. <path-to-your-env>/ocmtest/bin/activate`
* Install requirements: `pip install -r requirements.txt`
* Install docker
* Install htpasswd
* Install [crane](https://github.com/google/go-containerregistry/blob/main/cmd/crane/doc/crane.md)
* Create SSL certificates and store them in `./certs` directory (see `create-cert.sh` for instructions [Link](create-cert.sh) (be sure to have a hostname with a fully qualified domain name)
* Create a user for the OCI registry and passwd file: `htpasswd -b -v certs/htpasswd ocmuser <my-secret-password>`
* Set environment variables: `export FDQN_NAME=<Your fully qualified host-name>:4430; export USER_NAME=ocmuser; export PASSWD=<my-secret-password>`
* If you user alternative container runtimes to docker (like e.g. colima) you may need to set DOCKER_HOST env var. e.g.: `export DOCKER_HOST=unix:///Users/<my-user>/.colima/default/docker.sock`
* Run local OCI registry in docker: `./start_docker.sh`
* Build local test binaries: `./build.sh`
* Run tests: `pytest tests`
* Stop and remove container: `./stop_docker.sh`

## OCM v1 / v2 Compatibility Suite

The `compat/` directory holds a separate Go/Ginkgo suite that runs the same
OCM component-constructor through both the v1 (`open-component-model/ocm`)
and v2 (`open-component-model/open-component-model`) CLIs, through two
phases each (construct, then transfer). Cases are YAML files under
`compat/cases/`; expectations ride on `compat.ocm.software/*` labels. See
[`compat/README.md`](compat/README.md) for the layout, fixture system, and
how to add a case. CI runs it nightly via
[`.github/workflows/compat-test.yaml`](.github/workflows/compat-test.yaml).

## Contributing

This project is open to feature requests/suggestions, bug reports etc. via [GitHub issues](https://github.com/open-component-model/ocm-integrationtest/issues). Contribution and feedback are encouraged and always welcome. For more information about how to contribute, see our [Contributing Guide](https://ocm.software/community/contributing/).

## Code of Conduct

OCM follows the [NeoNephos Code of Conduct](https://github.com/neonephos/.github/blob/main/CODE_OF_CONDUCT.md).

## Licensing

Please see our [LICENSE](LICENSE) for copyright and license information.
Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/open-component-model/ocm-integrationtest).

## Test Report

[Latest Test Report](https://open-component-model.github.io/ocm-integrationtest/report.html)

## Statistics

Statistics of the latest [test runs](https://github.com/open-component-model/ocm-integrationtest/actions/workflows/integrationtest.yaml):

Date + Time | OCM Version | Result
------------|-------------|-------
2024-11-07 11:27:07+0000 | ocm version 0.17.0-dev+2188c1ce78e3cf6aaede3d458388fa947891c581 | &#9989; (passed)
2024-11-08 06:11:31+0000 | ocm version 0.17.0-dev+3577e7e9d72f25a84037aa34818a18c735e95d0a | &#9989; (passed)
2024-11-08 09:51:21+0000 | ocm version 0.17.0-dev+3577e7e9d72f25a84037aa34818a18c735e95d0a | &#9989; (passed)
2024-11-08 11:22:46+0000 | ocm version 0.18.0-dev+beabf65653edb08fff31c056e8645a4fdba75c72 | &#9989; (passed)
2024-11-08 13:38:35+0000 | ocm version 0.18.0-dev+2ea69c7ecca1e8be7e9d9f94dfdcac6090f1c69d | &#9989; (passed)
2024-11-08 18:53:47+0000 | ocm version 0.18.0-dev+2ea69c7ecca1e8be7e9d9f94dfdcac6090f1c69d | &#9989; (passed)
2024-11-08 19:39:22+0000 | ocm version 0.18.0-dev+2ea69c7ecca1e8be7e9d9f94dfdcac6090f1c69d | &#9989; (passed)
2024-11-09 06:10:19+0000 | ocm version 0.18.0-dev+b711f7eae82c8b17bca71166ea5a066b6061573c | &#9989; (passed)
2024-11-10 05:46:52+0000 | ocm version 0.18.0-dev+b711f7eae82c8b17bca71166ea5a066b6061573c | &#9989; (passed)
2024-11-10 06:10:35+0000 | ocm version 0.18.0-dev+b711f7eae82c8b17bca71166ea5a066b6061573c | &#9989; (passed)
2024-11-10 08:50:12+0000 | ocm version 0.18.0-dev+b711f7eae82c8b17bca71166ea5a066b6061573c | &#9989; (passed)
2024-11-10 09:11:22+0000 | ocm version 0.18.0-dev+b711f7eae82c8b17bca71166ea5a066b6061573c | &#9989; (passed)
2024-11-11 06:12:14+0000 | ocm version 0.18.0-dev+b711f7eae82c8b17bca71166ea5a066b6061573c | &#9989; (passed)
2024-11-11 08:09:22+0000 | ocm version 0.18.0-dev+284e158798079127086eeaef762d61045bc5a583 | &#9989; (passed)
2024-11-11 08:25:24+0000 | ocm version 0.18.0-dev+4bdd42657537734cb2eb850fb0b9a0f578a7cf06 | &#9989; (passed)
2024-11-12 06:11:18+0000 | ocm version 0.18.0-dev+ef17caf31be954488ea7f32610b10267218db620 | &#9989; (passed)
2024-11-12 11:12:48+0000 | ocm version 0.18.0-dev+1b607462f2b11ba6f1e554e1a688d5379752dbf6 | &#9989; (passed)
2024-11-12 14:10:51+0000 | ocm version 0.18.0-dev+782970c77f9da3ab8d73eb7904a50d0b1a9d1aba | &#9989; (passed)
2024-11-12 14:37:31+0000 | ocm version 0.18.0-dev+782970c77f9da3ab8d73eb7904a50d0b1a9d1aba | &#9989; (passed)
2024-11-13 06:11:33+0000 | ocm version 0.18.0-dev+a1890c261fc2a5ffb58ee1590662b04a752fa9a0 | &#9989; (passed)
2024-11-14 06:11:39+0000 | ocm version 0.19.0-dev+b3873862269e3d19f3e2c8532bbb38ded6a162d1 | &#9989; (passed)
2024-11-14 10:23:28+0000 | ocm version 0.19.0-dev+b3873862269e3d19f3e2c8532bbb38ded6a162d1 | &#9989; (passed)
2024-11-14 12:39:31+0000 | ocm version 0.19.0-dev+b3873862269e3d19f3e2c8532bbb38ded6a162d1 | &#9989; (passed)
2024-11-14 13:23:19+0000 | ocm version 0.19.0-dev+5447b0c55c00cff22c832b9ada51bd3942d5f56a | &#9989; (passed)
2024-11-14 13:30:31+0000 | ocm version 0.19.0-dev+5447b0c55c00cff22c832b9ada51bd3942d5f56a | &#9989; (passed)
2024-11-14 13:35:40+0000 | ocm version 0.19.0-dev+5447b0c55c00cff22c832b9ada51bd3942d5f56a | &#9989; (passed)
2024-11-15 06:12:14+0000 | ocm version 0.19.0-dev+5447b0c55c00cff22c832b9ada51bd3942d5f56a | &#9989; (passed)
2024-11-16 06:11:10+0000 | ocm version 0.19.0-dev+bb7c5f7639d35c2ab3b718571f80445a9a7fc3ca | &#9989; (passed)
2024-11-17 05:26:47+0000 | ocm version 0.19.0-dev+bb7c5f7639d35c2ab3b718571f80445a9a7fc3ca | &#9989; (passed)
2024-11-17 06:11:04+0000 | ocm version 0.19.0-dev+bb7c5f7639d35c2ab3b718571f80445a9a7fc3ca | &#9989; (passed)
2024-11-17 08:36:30+0000 | ocm version 0.19.0-dev+bb7c5f7639d35c2ab3b718571f80445a9a7fc3ca | &#9989; (passed)
2024-11-17 08:54:12+0000 | ocm version 0.19.0-dev+bb7c5f7639d35c2ab3b718571f80445a9a7fc3ca | &#9989; (passed)
2024-11-18 06:12:20+0000 | ocm version 0.19.0-dev+bb7c5f7639d35c2ab3b718571f80445a9a7fc3ca | &#9989; (passed)
2024-11-18 08:23:28+0000 | ocm version 0.19.0-dev+edadfe3417cf61e376183b58ccb6e29bd2a5ff11 | &#9989; (passed)
2024-11-18 08:38:55+0000 | ocm version 0.19.0-dev+74be7054fe6037a7ee6bb8b2a48486e03f26cee9 | &#9989; (passed)

---

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>