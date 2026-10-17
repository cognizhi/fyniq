# Changelog

## 0.1.0 (2026-10-17)


### Features

* **ai:** add the company-secretary agent skill ([e3d7494](https://github.com/cognizhi/fyniq/commit/e3d7494be1bede50e97baa9c0b63f9973f047579))
* **api,db:** add the second Super admin, recovery codes and two-person recovery ([#38](https://github.com/cognizhi/fyniq/issues/38)) ([6c6802d](https://github.com/cognizhi/fyniq/commit/6c6802debac8bdcc5e92c7bc248479544073c6ef))
* **api,db:** add users, memberships and first-run setup with the one-time code ([#37](https://github.com/cognizhi/fyniq/issues/37)) ([838601e](https://github.com/cognizhi/fyniq/commit/838601eff51c6f88ab18f8d06bfb3a3b6d3bffb8))
* **api,oauth:** sign in through Identity Platform in the release build ([#42](https://github.com/cognizhi/fyniq/issues/42)) ([171d98c](https://github.com/cognizhi/fyniq/commit/171d98cd0f2c47335b795609942c41244de79051))
* **api,web,db:** sign in to the web app with a first-party session ([#39](https://github.com/cognizhi/fyniq/issues/39)) ([160d1b0](https://github.com/cognizhi/fyniq/commit/160d1b0962769df34a284339e3e72d1b87828e2d))
* **api:** add the /v1 conventions, the first endpoints and the authorisation matrix ([#22](https://github.com/cognizhi/fyniq/issues/22)) ([2efb35f](https://github.com/cognizhi/fyniq/commit/2efb35fbfead99691c3319352888ce050eb586e5))
* **ci,security:** sign the release images and attest their SBOMs in CI ([#48](https://github.com/cognizhi/fyniq/issues/48)) ([c8397c0](https://github.com/cognizhi/fyniq/commit/c8397c067f0a7e7533de81a01183ff28af488283))
* **compliance:** add the control registry, the CTL-CHG-01 check and ASVS-tagged security tests ([#30](https://github.com/cognizhi/fyniq/issues/30)) ([4343f9d](https://github.com/cognizhi/fyniq/commit/4343f9d9fc8c9a8b4849dbfe6e229cb49fd6051d))
* **compliance:** add the nightly ledger verification, control evidence and log scrubbing across services ([#32](https://github.com/cognizhi/fyniq/issues/32)) ([7580f59](https://github.com/cognizhi/fyniq/commit/7580f59c8f7c7f9b2285f485b9c6098faabec4db))
* **db:** build the database backstop, the storage port and its attack suites ([#10](https://github.com/cognizhi/fyniq/issues/10)) ([ec2de9e](https://github.com/cognizhi/fyniq/commit/ec2de9e6273889821f1aa9f8cb090324bd7266d3))
* **db:** build the hash-chained audit trail, signed anchors and verification ([#15](https://github.com/cognizhi/fyniq/issues/15)) ([559631a](https://github.com/cognizhi/fyniq/commit/559631a5a5a67fab5885f8632587e2f16365130b))
* **deploy,ci:** load the sample company into the database in local mode ([#47](https://github.com/cognizhi/fyniq/issues/47)) ([277a425](https://github.com/cognizhi/fyniq/commit/277a425d35ec009dfe534c011d5492ef06e2fbeb))
* **deploy:** build the P0 repository skeleton and local mode ([#8](https://github.com/cognizhi/fyniq/issues/8)) ([780e497](https://github.com/cognizhi/fyniq/commit/780e4975f187d10dbea4363b4b68ed1ab5ac04af))
* **design:** add uipro-reviewed design system, tokens and clickable prototype ([2b93a00](https://github.com/cognizhi/fyniq/commit/2b93a00121dffeece04baeeae25d8900a6c9b3d4))
* **design:** show My claims, notifications, banners and the secretary in the prototype ([14056d4](https://github.com/cognizhi/fyniq/commit/14056d47374ba242d7cac74d01529eac95f44f2f))
* **engine,db,api:** enforce the manual journal rules PRD-001 rev 0.24 decided ([#40](https://github.com/cognizhi/fyniq/issues/40)) ([0d48f5f](https://github.com/cognizhi/fyniq/commit/0d48f5f970a0167f2f43a8bc18c5c299f36f44c3))
* **infra,ci:** write the Google Cloud infrastructure as OpenTofu, tested without a cloud account ([#49](https://github.com/cognizhi/fyniq/issues/49)) ([2773963](https://github.com/cognizhi/fyniq/commit/2773963d03662f3e55506750c7562a3e6a73302e))
* **infra,security:** add the Cloud KMS signer and Cloud Storage blob store ([#44](https://github.com/cognizhi/fyniq/issues/44)) ([4343005](https://github.com/cognizhi/fyniq/commit/43430055ba322386cfab14526032b4c85c4b81c3))
* **infra,worker:** run background work on Cloud Tasks and Cloud Run jobs ([#45](https://github.com/cognizhi/fyniq/issues/45)) ([e2edde3](https://github.com/cognizhi/fyniq/commit/e2edde3184217ffeab625477e2c1256ff17f703f))
* **kb:** add knowledge-base schema, tooling and first compiled pages ([c870532](https://github.com/cognizhi/fyniq/commit/c87053242a2b5dc395cce8de99734f371360c34a))
* **ledger:** build money, the rounding policies, the posting engine and the manual journal ([#13](https://github.com/cognizhi/fyniq/issues/13)) ([e4f0522](https://github.com/cognizhi/fyniq/commit/e4f0522b0de562257cb783ba52b06636b311963e))
* **mcp,oauth:** prove open question 40's option (b) with the JWT-bearer grant ([#43](https://github.com/cognizhi/fyniq/issues/43)) ([a1211cd](https://github.com/cognizhi/fyniq/commit/a1211cd12aaf532382d76eeb57c924b376e96637))
* **oauth:** pin Hydra, add the login and consent flow, and accept only API-audience tokens ([#33](https://github.com/cognizhi/fyniq/issues/33)) ([dc04190](https://github.com/cognizhi/fyniq/commit/dc041909e4bd4ea1f6608aa015d55d80f2c7743a))
* **oauth:** revoke a user's tokens after a change, and expire grants after 90 days ([#34](https://github.com/cognizhi/fyniq/issues/34)) ([3711a39](https://github.com/cognizhi/fyniq/commit/3711a397bbf5fd55d11db07c30496e7a381775aa))
* **oracle:** add the independent reference ledger and the differential scenario format ([#16](https://github.com/cognizhi/fyniq/issues/16)) ([6e3f484](https://github.com/cognizhi/fyniq/commit/6e3f4844d099f65c35e8058b33a97c6a9e4fdf2f))
* **oracle:** mark share-capital accounts and require a rate's source in fyniq-scenario@1 ([#36](https://github.com/cognizhi/fyniq/issues/36)) ([5121e0b](https://github.com/cognizhi/fyniq/commit/5121e0b6814020b03d35a3c12c9cca00e9b1c60a))
* **skills:** add fh-* agent skills and AGENTS.md/CLAUDE.md coding instructions ([fb219a4](https://github.com/cognizhi/fyniq/commit/fb219a4bcf52098e5b83ce1f59e043e9b7c453a1))
* **web,kb:** add the sign-in and first-run setup screens ([#41](https://github.com/cognizhi/fyniq/issues/41)) ([61e74d5](https://github.com/cognizhi/fyniq/commit/61e74d56a204606ddc2e31c755825c79458ad371))
* **worker:** add the Scheduler tick, platform jobs, heartbeats and wiring ([#21](https://github.com/cognizhi/fyniq/issues/21)) ([a3b7f81](https://github.com/cognizhi/fyniq/commit/a3b7f81639896c76266c4e442bb05028c1bf1d7a))
* **worker:** add the transactional outbox, push dispatch and the task handler ([#17](https://github.com/cognizhi/fyniq/issues/17)) ([eef2834](https://github.com/cognizhi/fyniq/commit/eef283428b0361c7601466a0fa3c05b9d90e675d))


### Bug fixes

* **api,db:** accept Idempotency-Key requests when the API's clock is not the database's ([#52](https://github.com/cognizhi/fyniq/issues/52)) ([142c8f1](https://github.com/cognizhi/fyniq/commit/142c8f1e8db2eb7b453ba597eebba69336826a2b))
* **db,security:** record the deployment recovery's actor as the migrate service ([#46](https://github.com/cognizhi/fyniq/issues/46)) ([85ede8b](https://github.com/cognizhi/fyniq/commit/85ede8b37b545297ce6f487f37bfda396fe593b8))
* **release:** start the first release at 0.1.0, not 1.0.0 ([8bcb8ff](https://github.com/cognizhi/fyniq/commit/8bcb8ff6afd216893511a21e387c742eb92d20bb))


### Documentation

* **kb:** add guide pages for claims in the personal workspace and Nemura leave ([d103a7c](https://github.com/cognizhi/fyniq/commit/d103a7ccb0751953d29ebeed2c38da2d92bf87c9))
* license Fyniq under MIT and mark the project a work in progress ([#11](https://github.com/cognizhi/fyniq/issues/11)) ([48de8a1](https://github.com/cognizhi/fyniq/commit/48de8a186c22eb63b22b275e63ac31c3ff461101))
* **oauth,specs:** drop the Ory licence quote and plan P0's close on local evidence ([#50](https://github.com/cognizhi/fyniq/issues/50)) ([74f72fe](https://github.com/cognizhi/fyniq/commit/74f72feba0b137b3dce85188471324f01f4ce7ad))
* **prd,design,skills:** add founders' money, equity, evidence and in-app help (rev 0.3) ([#2](https://github.com/cognizhi/fyniq/issues/2)) ([2d8d565](https://github.com/cognizhi/fyniq/commit/2d8d56511d9b924f56394aee3fc7affe53c3dd10))
* **prd,kb:** rule out vectors for the knowledge base (rev 0.5) ([b4889ba](https://github.com/cognizhi/fyniq/commit/b4889ba867ee2f3c91c2832bcfbbfb0e208c1076))
* **prd,skills,design,deploy:** rename the product to Fyniq ([56438e8](https://github.com/cognizhi/fyniq/commit/56438e8e6fc1d2ac8de6a91154225e837b26a74a))
* **prd,skills:** validate the deep review and apply it as rev 0.8 ([c25fd11](https://github.com/cognizhi/fyniq/commit/c25fd11c6a648823313fb58cb4e62f7251ed18d4))
* **prd,specs,design,kb:** build before the second reviews and flag unreviewed pages (rev 0.22) ([7369b46](https://github.com/cognizhi/fyniq/commit/7369b463fbd09de30f6b2abc872103fcc07a3021))
* **prd,specs,design:** add Nemura, the optional people platform ([9ee5ab6](https://github.com/cognizhi/fyniq/commit/9ee5ab6a8d9c233e0b9aae76805c04adf76252ad))
* **prd,specs,design:** apply the 14 Oct deep review and a personal workspace for claims ([96a82b5](https://github.com/cognizhi/fyniq/commit/96a82b52204bdb6a9e48db085f8295d3cc509f08))
* **prd,specs,design:** check every [verify] tag against official pages and plan the second review ([a4cf104](https://github.com/cognizhi/fyniq/commit/a4cf104ecce54504d0d63627a1099df0ea283a7c))
* **prd,specs,design:** cut every workspace to the person's rights and design the role screens ([da5e5a2](https://github.com/cognizhi/fyniq/commit/da5e5a2209461b3e7deccac831366b40b285cc26))
* **prd,specs,design:** keep the PRD as specs and screens; resolve findings; multi-provider AI ([96ef8da](https://github.com/cognizhi/fyniq/commit/96ef8da6de12d77bb4ef9eabda7f4865bdf358c5))
* **prd,specs,design:** probe models before allowing them; price from LiteLLM's catalogue ([3976c36](https://github.com/cognizhi/fyniq/commit/3976c369c6134bb2c33bfa668285ce3b5b2f878b))
* **prd,specs,design:** read each connection's models from the provider, not a built-in list ([6511fb7](https://github.com/cognizhi/fyniq/commit/6511fb7d5f1cbf8c24852d18810e4fdcaf47a463))
* **prd,specs:** accept the architecture decisions P0 builds on (rev 0.21) ([4672897](https://github.com/cognizhi/fyniq/commit/4672897be590693786cb47c4dc697ec691fdc0ab))
* **prd,specs:** decide the rules a P0 manual journal and its reversal post by (rev 0.24) ([#27](https://github.com/cognizhi/fyniq/issues/27)) ([21c6653](https://github.com/cognizhi/fyniq/commit/21c6653488e43c5481cc0349765ff9cea8ae6910))
* **prd,specs:** plan P0 foundation as eleven OpenSpec changes ([af4eb54](https://github.com/cognizhi/fyniq/commit/af4eb54a7cd1344c8fc10d9ebc55c7cf2db455a0))
* **prd,specs:** plan P0 foundation as eleven OpenSpec changes ([cfa58ac](https://github.com/cognizhi/fyniq/commit/cfa58ac4f17c459461b4da9d16f45a1b0ee59017))
* **prd,specs:** record the founders' decisions of 16 Oct 2026 (PRD-001 rev 0.25, PRD-002 rev 0.7) ([#55](https://github.com/cognizhi/fyniq/issues/55)) ([4394763](https://github.com/cognizhi/fyniq/commit/43947639a6fa92d2620c433ff1754aa1ec4b6531))
* **prd:** add AI company secretary, Xero parity, sign-in and notifications (rev 0.6) ([21257e7](https://github.com/cognizhi/fyniq/commit/21257e7b6d836004984ca5a6a83b2324a0aad810))
* **prd:** add AI context and a compiled knowledge base, ADR-T6 (rev 0.4) ([c0a332a](https://github.com/cognizhi/fyniq/commit/c0a332a1e3c5f9d08edec76add917bf66f8a6e70))
* **prd:** add bank sync, payments, templates, claims, projects, connectors, portability (rev 0.7) ([b8d2d81](https://github.com/cognizhi/fyniq/commit/b8d2d812335a71c27e8c5e52a24d4bcd62ab00f7))
* **prd:** apply deep review findings to PRD-001 (rev 0.2) ([0f529a3](https://github.com/cognizhi/fyniq/commit/0f529a3fa5efc5a626f1ce015cbfa6859e241591))
* **prd:** decide the seven open questions P0 needs and accept ADR-T3 ([5f0baa6](https://github.com/cognizhi/fyniq/commit/5f0baa6e625e16d271eb8191b63a2af331d77a52))
* **skills:** add fh-knowledge-base and point instructions at kb/ ([9335416](https://github.com/cognizhi/fyniq/commit/9335416df58348b21b9f2e46fb6b2e6f06e35167))
* **skills:** add fyniq-openspec and use sub-agents only for short tasks ([202732d](https://github.com/cognizhi/fyniq/commit/202732dccae99effecc838dc906ffc146555583c))
* **skills:** correct how release-please picks the first version ([cae0fd7](https://github.com/cognizhi/fyniq/commit/cae0fd784144277269e324d3e1c3253faf6a877c))
* **skills:** cover connectors, the outbox and cloud portability ([9d849a7](https://github.com/cognizhi/fyniq/commit/9d849a77c484001b2c5571c979c5188b5a3afe9c))
* **skills:** cover scheduled agents, integration adapters and the ACRA pack ([a26b0d2](https://github.com/cognizhi/fyniq/commit/a26b0d21b83714c806167d313a0958b326d0d45f))
* **skills:** point agents at Nemura and add the people assistant ([cde261b](https://github.com/cognizhi/fyniq/commit/cde261b27ed45dcf8385694bdf19f56d461b9bce))
* **skills:** point agents at the 14 Oct review and keep pay figures out of the assistant ([c8da8d6](https://github.com/cognizhi/fyniq/commit/c8da8d6ac3bee46d2d9bdbf29882eb383a1ac779))
* **specs:** archive the P0 API conventions and authorisation change ([#24](https://github.com/cognizhi/fyniq/issues/24)) ([67945a6](https://github.com/cognizhi/fyniq/commit/67945a6ab963af076cba101df2186ab8ad6245a6))
* **specs:** archive the P0 money engine and oracle changes ([#20](https://github.com/cognizhi/fyniq/issues/20)) ([d509700](https://github.com/cognizhi/fyniq/commit/d509700e424b26dc3e314b93ac4e3180b1da1aea))
* **specs:** archive the P0 skeleton and database tenancy changes ([#9](https://github.com/cognizhi/fyniq/issues/9)) ([a6019b7](https://github.com/cognizhi/fyniq/commit/a6019b7a9ee30d4362b66f6d8e85bc2e4def6177))
* **specs:** generate the P1–P3 plan's tables with make roadmap and check them in make check ([#54](https://github.com/cognizhi/fyniq/issues/54)) ([9e917f3](https://github.com/cognizhi/fyniq/commit/9e917f39edce4eb8df7e6eba5f07d688b48f01e2))
* **specs:** plan P1, P2 and P3 after P0, local first ([#51](https://github.com/cognizhi/fyniq/issues/51)) ([3146d17](https://github.com/cognizhi/fyniq/commit/3146d175f401556c2da766c8ed87224573d31c23))
* **specs:** propose p0-manual-journal-rules for the rules PRD rev 0.24 decided ([#35](https://github.com/cognizhi/fyniq/issues/35)) ([3175ab7](https://github.com/cognizhi/fyniq/commit/3175ab7690560d4ebc30e156c13d64585a521d03))


### Build and images

* **design:** build Nemura screens and check its tokens alongside Fyniq's ([82df153](https://github.com/cognizhi/fyniq/commit/82df153aa1a11004e791c2571ffe4aa110c87661))
* **design:** draw Fyniq's personal workspace for people who only claim ([b699429](https://github.com/cognizhi/fyniq/commit/b699429bafa5e954229e7933618fc8654fc00340))
* **design:** show the Finance | People switch in every finance shell and add role personas ([a233235](https://github.com/cognizhi/fyniq/commit/a2332354e9bf6237406044064cc21c86a682b604))
* **release:** bootstrap release-please, commit checks and local dependencies ([f8980c7](https://github.com/cognizhi/fyniq/commit/f8980c7388982e5c4525a18413a5534231ccfbd1))
* **specs,design:** add the OpenSpec, spec-drift and screen checks to make check ([1954c0e](https://github.com/cognizhi/fyniq/commit/1954c0e95d91c8ed4848a6842876635ce1199a6b))


### CI

* auto-approve the owner's development pull requests before production ([#12](https://github.com/cognizhi/fyniq/issues/12)) ([fe950f8](https://github.com/cognizhi/fyniq/commit/fe950f8661d0494a34c17a11ebcce09cb1f897af))
* **deploy:** check the images against their budgets and hardening, scan them, and run local mode in CI ([#28](https://github.com/cognizhi/fyniq/issues/28)) ([45412ee](https://github.com/cognizhi/fyniq/commit/45412eea270d75c0713d281af9b1794fab52a94e))
* **deps:** stop Dependabot proposing major versions of images ([#31](https://github.com/cognizhi/fyniq/issues/31)) ([513d75a](https://github.com/cognizhi/fyniq/commit/513d75a5cca9f63a2ea3a0a21a75dcc895ef3a6b))
* **release:** run release-please with the release App's token so CI runs on release pull requests ([#53](https://github.com/cognizhi/fyniq/issues/53)) ([629e714](https://github.com/cognizhi/fyniq/commit/629e7145f74b7b93dcda24f5647d587587ca531c))
* **security:** gate merges on security scans and the workflow supply-chain rules ([#25](https://github.com/cognizhi/fyniq/issues/25)) ([16fcadb](https://github.com/cognizhi/fyniq/commit/16fcadbb0b46416fd1ea8575f0f24ed9fc1e5e9f))

## Changelog

All notable changes to Cognizhi Finance Hub Cloud.

- **Generated by [release-please](https://github.com/googleapis/release-please)** from Conventional
  Commits on `main`. Do not edit versions or this file by hand (PRD-001 §12.10, REL-02).
- Versions follow SemVer. While the version is `0.x` (before go-live), `feat` bumps the minor
  version and breaking changes also bump the minor version.
- Anything users, operators or integrators must do appears under **Upgrade notes**, taken from
  `BREAKING CHANGE:` footers and "Upgrade notes:" paragraphs in commit bodies (REL-03).
