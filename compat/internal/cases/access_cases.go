// Access-type compat cases. One entry per registered access spec on
// v1's side; the constructor bodies exercise every alias v1 recognises
// so a rename on either leg surfaces as a per-resource failure. The
// raw constructors are byte-for-byte the YAML a user would author.
package cases

func init() {
	registerCases("access",
		accessGitHub,
		accessHelm,
		accessMaven,
		accessNPM,
		accessOCIImage,
		accessOCIImageLayer,
		accessS3,
		accessWget,
	)
}

var accessGitHub = &Case{
	ID: "access:GitHub",
	Notes: "canonical GitHub/v1 plus spelling variants. v1 supports the type; " +
		"v2 ships no GitHub access plugin today, so every spelling fails with " +
		`failed to get plugin for typ "..."` + ". Once v2 implements the plugin, " +
		"flip the v2 construct/transfer expectations to pass.",
	Expect: Expectation{
		Construct: LegExpect{V1: Pass(), V2: FailKind("v2:access_plugin_missing")},
		Transfer:  LegExpect{V1: Pass(), V2: Skip("")},
	},
	Constructor: `components:
- name: ocm.software/compat/access/github
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: r-gh-canonical
    type: source
    version: 6.7.1
    relation: external
    access:
      type: GitHub/v1
      repoUrl: https://github.com/stefanprodan/podinfo
      commit: 9bd1c2dab3d0b8527c12f8bcb716b1d40dbeed18
  - name: r-gh-canonical-noversion
    type: source
    version: 6.7.1
    relation: external
    access:
      type: GitHub
      repoUrl: https://github.com/stefanprodan/podinfo
      commit: 9bd1c2dab3d0b8527c12f8bcb716b1d40dbeed18
  - name: r-gh-camel-alias
    type: source
    version: 6.7.1
    relation: external
    access:
      type: gitHub
      repoUrl: https://github.com/stefanprodan/podinfo
      commit: 9bd1c2dab3d0b8527c12f8bcb716b1d40dbeed18
  - name: r-gh-camel-v1-alias
    type: source
    version: 6.7.1
    relation: external
    access:
      type: gitHub/v1
      repoUrl: https://github.com/stefanprodan/podinfo
      commit: 9bd1c2dab3d0b8527c12f8bcb716b1d40dbeed18
  - name: r-gh-lower-alias
    type: source
    version: 6.7.1
    relation: external
    access:
      type: github
      repoUrl: https://github.com/stefanprodan/podinfo
      commit: 9bd1c2dab3d0b8527c12f8bcb716b1d40dbeed18
`,
}

var accessHelm = &Case{
	ID:    "access:Helm",
	Notes: "canonical Helm/v1. Both legs accept all four spellings.",
	Expect: Expectation{
		Construct: LegExpect{V1: Pass(), V2: Pass()},
		Transfer:  LegExpect{V1: Pass(), V2: Pass()},
	},
	Constructor: `components:
- name: ocm.software/compat/access/helm
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: r-helm-canonical
    type: helmChart
    version: 6.7.1
    relation: external
    access:
      type: Helm/v1
      helmChart: podinfo:6.7.1
      helmRepository: https://stefanprodan.github.io/podinfo
  - name: r-helm-canonical-noversion
    type: helmChart
    version: 6.7.1
    relation: external
    access:
      type: Helm
      helmChart: podinfo:6.7.1
      helmRepository: https://stefanprodan.github.io/podinfo
  - name: r-helm-lower-alias
    type: helmChart
    version: 6.7.1
    relation: external
    access:
      type: helm
      helmChart: podinfo:6.7.1
      helmRepository: https://stefanprodan.github.io/podinfo
  - name: r-helm-lower-v1-alias
    type: helmChart
    version: 6.7.1
    relation: external
    access:
      type: helm/v1
      helmChart: podinfo:6.7.1
      helmRepository: https://stefanprodan.github.io/podinfo
`,
}

var accessMaven = &Case{
	ID: "access:Maven",
	Notes: "not in ocm-spec, but v1 ships a Maven access plugin and v2 ships none " +
		"today. All four spellings fail on v2 with the same plugin-not-found prefix. " +
		"Once v2 implements the plugin, flip the v2 construct/transfer expectations " +
		"to pass.",
	Fixtures: []FixtureSpec{{
		Name: "MAVEN",
		Kind: "hermeticMavenRepo",
		With: map[string]any{
			"group":    "org.apache.commons",
			"artifact": "commons-lang3",
			"version":  "3.14.0",
			"jar":      "compat access:Maven\n",
		},
	}},
	Expect: Expectation{
		Construct: LegExpect{V1: Pass(), V2: FailKind("v2:access_plugin_missing")},
		Transfer:  LegExpect{V1: Pass(), V2: Skip("")},
	},
	Constructor: `components:
- name: ocm.software/compat/access/maven
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: r-mvn-canonical
    type: jar
    version: 3.14.0
    relation: external
    access:
      type: Maven/v1
      repoUrl: ${MAVEN.url}
      groupId: org.apache.commons
      artifactId: commons-lang3
      version: "3.14.0"
  - name: r-mvn-canonical-noversion
    type: jar
    version: 3.14.0
    relation: external
    access:
      type: Maven
      repoUrl: ${MAVEN.url}
      groupId: org.apache.commons
      artifactId: commons-lang3
      version: "3.14.0"
  - name: r-mvn-lower-alias
    type: jar
    version: 3.14.0
    relation: external
    access:
      type: maven
      repoUrl: ${MAVEN.url}
      groupId: org.apache.commons
      artifactId: commons-lang3
      version: "3.14.0"
  - name: r-mvn-lower-v1-alias
    type: jar
    version: 3.14.0
    relation: external
    access:
      type: maven/v1
      repoUrl: ${MAVEN.url}
      groupId: org.apache.commons
      artifactId: commons-lang3
      version: "3.14.0"
`,
}

var accessNPM = &Case{
	ID: "access:NPM",
	Notes: "canonical NPM/v1 plus three spelling variants. v1 supports the type; " +
		"v2 ships no NPM access plugin today, so every spelling fails with " +
		`failed to get plugin for typ "..."` + ". Once v2 implements the plugin, " +
		"flip the v2 construct/transfer expectations to pass; all four spellings " +
		"will pass together.",
	Expect: Expectation{
		Construct: LegExpect{V1: Pass(), V2: FailKind("v2:access_plugin_missing")},
		Transfer:  LegExpect{V1: Pass(), V2: Skip("")},
	},
	Constructor: `components:
- name: ocm.software/compat/access/npm
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: r-npm-canonical
    type: npmPackage
    version: "5.0.0"
    relation: external
    access:
      type: NPM/v1
      registry: https://registry.npmjs.org
      package: yallist
      version: "5.0.0"
  - name: r-npm-canonical-noversion
    type: npmPackage
    version: "5.0.0"
    relation: external
    access:
      type: NPM
      registry: https://registry.npmjs.org
      package: yallist
      version: "5.0.0"
  - name: r-npm-lower-alias
    type: npmPackage
    version: "5.0.0"
    relation: external
    access:
      type: npm
      registry: https://registry.npmjs.org
      package: yallist
      version: "5.0.0"
  - name: r-npm-lower-v1-alias
    type: npmPackage
    version: "5.0.0"
    relation: external
    access:
      type: npm/v1
      registry: https://registry.npmjs.org
      package: yallist
      version: "5.0.0"
`,
}

var accessOCIImage = &Case{
	ID: "access:OCIImage",
	Notes: "canonical OCIImage/v1. Both legs accept the spec form plus the legacy " +
		"aliases ociArtifact and ociRegistry. Note that `ociImage` (camelCase with " +
		"lowercase 'i') is NOT a registered alias on either side; only `OCIImage` exists.",
	Fixtures: []FixtureSpec{{
		Name: "image",
		Kind: "ociArtifact",
		With: map[string]any{
			"repo":    "compat/access-oci",
			"tag":     "v1",
			"payload": "OCIImage access compat\n",
		},
	}},
	Expect: Expectation{
		Construct: LegExpect{V1: Pass(), V2: Pass()},
		Transfer:  LegExpect{V1: Pass(), V2: Pass()},
	},
	Constructor: `components:
- name: ocm.software/compat/access/ociartifact
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: r-oci-image
    type: ociImage
    version: 1.0.0
    relation: external
    access:
      type: OCIImage/v1
      imageReference: ${image.ref}
  - name: r-oci-image-unversioned
    type: ociImage
    version: 1.0.0
    relation: external
    access:
      type: OCIImage
      imageReference: ${image.ref}
  - name: r-oci-artifact-alias
    type: ociImage
    version: 1.0.0
    relation: external
    access:
      type: ociArtifact
      imageReference: ${image.ref}
  - name: r-oci-artifact-v1-alias
    type: ociImage
    version: 1.0.0
    relation: external
    access:
      type: ociArtifact/v1
      imageReference: ${image.ref}
  - name: r-oci-registry-alias
    type: ociImage
    version: 1.0.0
    relation: external
    access:
      type: ociRegistry
      imageReference: ${image.ref}
`,
}

var accessOCIImageLayer = &Case{
	ID: "access:OCIImageLayer",
	Notes: "layer-level OCI access. v1 registers all four spellings " +
		"(OCIImageLayer/v1, OCIImageLayer, ociBlob, ociBlob/v1) as aliases for the " +
		"same access method, which targets a single layer blob by digest inside an " +
		"existing repository. v1 needs an oci.config.ocm.software/v1 alias to talk " +
		"plain-HTTP to the testcontainers registry; the ociArtifact fixture wires " +
		"that automatically via V1PlainHTTPConfigArgs. v2 decodes all four spellings " +
		"(no plugin miss) but the v2 digest processor rejects each one with " +
		"'unsupported access type <spelling>: expected OCI image' because the " +
		"resource declares type: blob, not ociImage. The check is tied to the " +
		"resource type, not the access type, so even though v2 understands the spec " +
		"it refuses to digest it through the OCIImageLayer code path. errSubstr " +
		"matches the spelling-independent suffix. The fixture pushes a one-layer " +
		"image; image.layerRef is the bare host:port/repo form OCIImageLayer's ref: " +
		"field expects, distinct from OCIImage's image.ref (which is the full " +
		"repo:tag). size is omitted on purpose: v1 treats absent/zero size as " +
		"'unknown' and resolves from the registry.",
	Fixtures: []FixtureSpec{{
		Name: "image",
		Kind: "ociArtifact",
		With: map[string]any{
			"repo":    "compat/access-oci-layer",
			"tag":     "v1",
			"payload": "OCIImageLayer access compat\n",
		},
	}},
	Expect: Expectation{
		Construct: LegExpect{V1: Pass(), V2: FailKind("v2:expected_oci_image")},
		Transfer:  LegExpect{V1: Pass(), V2: Skip("")},
	},
	Constructor: `components:
- name: ocm.software/compat/access/ociimagelayer
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: r-oil-canonical
    type: blob
    version: 1.0.0
    relation: external
    access:
      type: OCIImageLayer/v1
      ref: ${image.layerRef}
      mediaType: ${image.layerMedia}
      digest: ${image.layerDigest}
  - name: r-oil-canonical-noversion
    type: blob
    version: 1.0.0
    relation: external
    access:
      type: OCIImageLayer
      ref: ${image.layerRef}
      mediaType: ${image.layerMedia}
      digest: ${image.layerDigest}
  - name: r-oil-ociblob-alias
    type: blob
    version: 1.0.0
    relation: external
    access:
      type: ociBlob
      ref: ${image.layerRef}
      mediaType: ${image.layerMedia}
      digest: ${image.layerDigest}
  - name: r-oil-ociblob-v1-alias
    type: blob
    version: 1.0.0
    relation: external
    access:
      type: ociBlob/v1
      ref: ${image.layerRef}
      mediaType: ${image.layerMedia}
      digest: ${image.layerDigest}
`,
}

var accessS3 = &Case{
	ID: "access:S3",
	Notes: "canonical S3/v1 plus three spelling variants. v1 supports the type; " +
		"v2 ships no S3 access plugin today, so every spelling fails with " +
		`failed to get plugin for typ "..."` + ". Once v2 implements the plugin, " +
		"flip the v2 construct/transfer expectations to pass. Bucket name compat.s3 " +
		"deliberately contains a dot so aws-sdk-go-v2 falls back to path-style " +
		"addressing (no env knob for UsePathStyle).",
	Fixtures: []FixtureSpec{{
		Name: "S3",
		Kind: "s3",
		With: map[string]any{
			"bucket":  "compat.s3",
			"key":     "payload.txt",
			"region":  "us-east-1",
			"payload": "compat access:S3\n",
		},
	}},
	Expect: Expectation{
		Construct: LegExpect{V1: Pass(), V2: FailKind("v2:access_plugin_missing")},
		Transfer:  LegExpect{V1: Pass(), V2: Skip("")},
	},
	Constructor: `components:
- name: ocm.software/compat/access/s3
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: r-s3-canonical
    type: blob
    version: 1.0.0
    relation: external
    access:
      type: S3/v1
      region: ${S3.region}
      bucket: ${S3.bucket}
      key: ${S3.key}
  - name: r-s3-canonical-noversion
    type: blob
    version: 1.0.0
    relation: external
    access:
      type: S3
      region: ${S3.region}
      bucket: ${S3.bucket}
      key: ${S3.key}
  - name: r-s3-lower-alias
    type: blob
    version: 1.0.0
    relation: external
    access:
      type: s3
      region: ${S3.region}
      bucket: ${S3.bucket}
      key: ${S3.key}
  - name: r-s3-lower-v1-alias
    type: blob
    version: 1.0.0
    relation: external
    access:
      type: s3/v1
      region: ${S3.region}
      bucket: ${S3.bucket}
      key: ${S3.key}
`,
}

var accessWget = &Case{
	ID: "access:Wget",
	Notes: "canonical Wget/v1 plus three spelling variants. v1 supports the type; " +
		"v2 ships no Wget access plugin today, so every spelling fails with " +
		`failed to get plugin for typ "..."` + ". Once v2 implements the plugin, " +
		"flip the v2 construct/transfer expectations to pass.",
	Fixtures: []FixtureSpec{{
		Name: "HTTP",
		Kind: "httpFileServer",
		With: map[string]any{
			"files": map[string]any{
				"payload.txt": "Wget access compat\n",
			},
		},
	}},
	Expect: Expectation{
		Construct: LegExpect{V1: Pass(), V2: FailKind("v2:access_plugin_missing")},
		Transfer:  LegExpect{V1: Pass(), V2: Skip("")},
	},
	Constructor: `components:
- name: ocm.software/compat/access/wget
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: r-wget-canonical
    type: plainText
    version: 1.0.0
    relation: external
    access:
      type: Wget/v1
      url: ${HTTP.payload.txt.url}
      mediaType: text/plain
  - name: r-wget-canonical-noversion
    type: plainText
    version: 1.0.0
    relation: external
    access:
      type: Wget
      url: ${HTTP.payload.txt.url}
      mediaType: text/plain
  - name: r-wget-lower-alias
    type: plainText
    version: 1.0.0
    relation: external
    access:
      type: wget
      url: ${HTTP.payload.txt.url}
      mediaType: text/plain
  - name: r-wget-lower-v1-alias
    type: plainText
    version: 1.0.0
    relation: external
    access:
      type: wget/v1
      url: ${HTTP.payload.txt.url}
      mediaType: text/plain
`,
}
