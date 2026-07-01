// Input-type compat cases. One entry per input specification kind v1
// recognises; each constructor exercises a single canonical spelling
// (input kinds don't have the alias sprawl access types do). Raw
// constructor bodies are byte-for-byte the YAML a user would author.
package cases

func init() {
	registerCases("inputs",
		inputDir,
		inputFile,
		inputGit,
		inputHelm,
		inputMaven,
		inputNPM,
		inputOCIArtifact,
		inputUTF8,
		inputWget,
	)
}

var inputDir = &Case{
	ID:    "input:dir",
	Notes: "directory packaged as tar+gzip; both legs supported.",
	Fixtures: []FixtureSpec{{
		Name: "dir",
		Kind: "writeDir",
		With: map[string]any{
			"path": "payload-dir",
			"files": map[string]any{
				"a.txt":     "a\n",
				"sub/b.txt": "b\n",
			},
		},
	}},
	Expect: Expectation{
		Construct: LegExpect{V1: Pass(), V2: Pass()},
		Transfer:  LegExpect{V1: Pass(), V2: Pass()},
	},
	Constructor: `components:
- name: ocm.software/compat/input/dir
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: r-dir
    type: directory
    version: 1.0.0
    relation: local
    input:
      type: dir
      path: ${dir.path}
      mediaType: application/x-tar
      compress: true
`,
}

var inputFile = &Case{
	ID:    "input:file",
	Notes: "single on-disk file; both legs supported.",
	Fixtures: []FixtureSpec{{
		Name: "file",
		Kind: "writeFile",
		With: map[string]any{
			"path": "payload.txt",
			"data": "file input compat\n",
		},
	}},
	Expect: Expectation{
		Construct: LegExpect{V1: Pass(), V2: Pass()},
		Transfer:  LegExpect{V1: Pass(), V2: Pass()},
	},
	Constructor: `components:
- name: ocm.software/compat/input/file
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: r-file
    type: plainText
    version: 1.0.0
    relation: local
    input:
      type: file
      mediaType: text/plain
      path: ${file.path}
`,
}

var inputGit = &Case{
	ID: "input:git",
	Notes: "v1's git input shells out to git, which is not present in the " +
		"ghcr.io/open-component-model/ocm image. Skipped both legs.",
	Expect: Expectation{
		Construct: LegExpect{
			V1: Skip("v1 git input requires `git` binary, not present in docker image"),
			V2: Skip("no v2 plugin"),
		},
		Transfer: LegExpect{
			V1: Skip("depends on construct"),
			V2: Skip("depends on construct"),
		},
	},
	Constructor: `components:
- name: ocm.software/compat/input/git
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: r-git
    type: plainText
    version: 1.0.0
    relation: local
    input:
      type: git
      repository: file:///placeholder
`,
}

var inputHelm = &Case{
	ID:    "input:helm",
	Notes: "local helm chart; both legs supported.",
	Fixtures: []FixtureSpec{{
		Name: "chart",
		Kind: "writeChart",
		With: map[string]any{
			"path":    "chart",
			"name":    "compat",
			"version": "1.0.0",
		},
	}},
	Expect: Expectation{
		Construct: LegExpect{V1: Pass(), V2: Pass()},
		Transfer:  LegExpect{V1: Pass(), V2: Pass()},
	},
	Constructor: `components:
- name: ocm.software/compat/input/helm
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: r-helm
    type: helmChart
    version: 1.0.0
    relation: local
    input:
      type: helm
      path: ${chart.path}
`,
}

var inputMaven = &Case{
	ID: "input:maven",
	Notes: "hermetic Maven repo (httptest, not Maven Central; it rate-limits and " +
		"the project is called out by name). v2 has no plugin.",
	Fixtures: []FixtureSpec{{
		Name: "MAVEN",
		Kind: "hermeticMavenRepo",
		With: map[string]any{
			"group":    "org.apache.commons",
			"artifact": "commons-lang3",
			"version":  "3.14.0",
			"jar":      "compat input:maven\n",
		},
	}},
	Expect: Expectation{
		Construct: LegExpect{V1: Pass(), V2: FailKind("v2:input_kind_unsupported")},
		Transfer:  LegExpect{V1: Pass(), V2: Skip("")},
	},
	Constructor: `components:
- name: ocm.software/compat/input/maven
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: r-maven
    type: jar
    version: 1.0.0
    relation: local
    input:
      type: maven
      repoUrl: ${MAVEN.url}
      groupId: org.apache.commons
      artifactId: commons-lang3
      version: "3.14.0"
`,
}

var inputNPM = &Case{
	ID:    "input:npm",
	Notes: "public npm registry. v2 has no plugin.",
	Expect: Expectation{
		Construct: LegExpect{V1: Pass(), V2: FailKind("v2:input_kind_unsupported")},
		Transfer:  LegExpect{V1: Pass(), V2: Skip("")},
	},
	Constructor: `components:
- name: ocm.software/compat/input/npm
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: r-npm
    type: npmPackage
    version: 1.0.0
    relation: local
    input:
      type: npm
      registry: https://registry.npmjs.org
      package: yallist
      version: "5.0.0"
`,
}

var inputOCIArtifact = &Case{
	ID:    "input:ociArtifact",
	Notes: "pull tiny artifact from per-process registry. v2 has no plugin.",
	Fixtures: []FixtureSpec{{
		Name: "image",
		Kind: "ociArtifact",
		With: map[string]any{
			"repo":    "compat/input-oci",
			"tag":     "v1",
			"payload": "compat input oci\n",
		},
	}},
	Expect: Expectation{
		Construct: LegExpect{V1: Pass(), V2: FailKind("v2:input_kind_unsupported")},
		Transfer:  LegExpect{V1: Pass(), V2: Skip("")},
	},
	Constructor: `components:
- name: ocm.software/compat/input/ociartifact
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: r-input-oci
    type: ociImage
    version: 1.0.0
    relation: local
    input:
      type: ociArtifact
      path: ${image.ref}
`,
}

var inputUTF8 = &Case{
	ID:    "input:utf8",
	Notes: "inline UTF-8 text; both legs supported.",
	Expect: Expectation{
		Construct: LegExpect{V1: Pass(), V2: Pass()},
		Transfer:  LegExpect{V1: Pass(), V2: Pass()},
	},
	Constructor: `components:
- name: ocm.software/compat/input/utf8
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: r-utf8
    type: plainText
    version: 1.0.0
    relation: local
    input:
      type: utf8
      mediaType: text/plain
      text: "utf8 compat payload\n"
`,
}

var inputWget = &Case{
	ID:    "input:wget",
	Notes: "fetch from local HTTP server. v2 has no plugin.",
	Fixtures: []FixtureSpec{{
		Name: "HTTP",
		Kind: "httpFileServer",
		With: map[string]any{
			"files": map[string]any{
				"payload.txt": "wget compat\n",
			},
		},
	}},
	Expect: Expectation{
		Construct: LegExpect{V1: Pass(), V2: FailKind("v2:input_kind_unsupported")},
		Transfer:  LegExpect{V1: Pass(), V2: Skip("")},
	},
	Constructor: `components:
- name: ocm.software/compat/input/wget
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: r-wget
    type: plainText
    version: 1.0.0
    relation: local
    input:
      type: wget
      url: ${HTTP.payload.txt.url}
      mediaType: text/plain
`,
}
