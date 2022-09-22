# TNF & OCT integration roadmap/tasks proposal

In order to make TNF to use the OCT container image, I propose to do it incrementally and in a way that TNF's Github CI needs minimum updates, both for normal checks and TNF's container image creation. These are the main goals:
1. Decouple the offline catalog downloading & parsing from TNF's code/repo.
2. Make TNF to download and use the latest OCT container image to:
    1. Download the latest catalog from OCT so TNF's UTs can pass.
    2. Use the catalog files from OCT when creating the final TNF's container image.

Ideally, TNF changes should be made first, and they should be aimed to remove the fetch tool as the final step.

## Roadmap proposal
This is the proposed roadmap:
1. TNF needs to be changed to use an arbitrary offline catalog folder, but using the existing TNF's offline check function implementations.
2. We need to modify github workflows to use oct container to generate the offline catalog files in a local folder, then make TNF to run with that folder as a param.
3. Move the offline api implementation functions into OCT repo so TNF can use import and use them. TNF's can remain but the offline implementation should use OCT's functions.

## TNF
### Current implementation
TNF stores the catalog files under the cmd/fetch/data folder, which has this structure:
```
cmd/tnf/fetch/data
├── cmd/tnf/fetch/data/archive.json
├── cmd/tnf/fetch/data/containers
│   └── cmd/tnf/fetch/data/containers/containers.db
├── cmd/tnf/fetch/data/helm
│   └── cmd/tnf/fetch/data/helm/helm.db
└── cmd/tnf/fetch/data/operators
    ├── cmd/tnf/fetch/data/operators/operator_catalog_page_0_100.db
    ├── cmd/tnf/fetch/data/operators/operator_catalog_page_1_100.db
    ...
```

There's a package in the TNF repo called `offlinecheck` that implements the api functions that check the certification status for containers, operators and helm chart releases. This api is common for both online and offline checks. Before any TC of the affiliated-certification TS can run, the "offline" catalog is loaded:
```
	ginkgo.BeforeEach(func() {
		env = provider.GetTestEnvironment()
		api.LoadCatalog()
	})
```
That api function (`LoadCatalog`) doesn't make any sense for online checking, it just calls the offline function to load & parse the catalog files from the hardcoded path `cmd/tnf/fetch/data/`.

### Tasks

1. Make TNF to read/parse arbitrary db folder.
    - Update internal/registry to use a db folder passed as a flag.
    - Make that param to point to the existing cmd/tnf/fetch/data folder to allow backwards compatiblity. Github workflows shouldn't be changed yet.
2. Change TNF workflows for image creation.
    - Download & run latest OCT container.
    - Copy newly downloaded offline catalog content into cmd/tnf/fetch/data
3. Remove fetch tool from TNF.
4. Remove TNF's offline function implementations and use OCTs ones.

## OCT
### Tasks
1. Move TNF's internal/registry api (offline funcitons implementations) to OCT.
2. Improve OCT's fetch tool. There'are a lot of improvents that can be done in the existin TNF code for the fetch tool. To name some of them:
    - Operators are currently downloading every field from the catalog, but we only need some of them. Also, they're saved on disk in a file for each http response. We can merge everything into a single file as with the containers.
    - The size of this db files will increase monotonically, as new entries are added every day. Maybe saving everything in json format (either in single or in multiple files) is not the best way to go for future-proof. We might need to put a size limit on this files. SQLite can be explored as an alternative here.
    - We download sequentially, page by page, but we could issue several queries in parallel (using go-routines).

### Optionally
1. Separate OCT repo in two repositories? Is it really worth it?
    1. oct : just the fetch app.
    2. oct-api : api to be used by both OCT and TNF.

