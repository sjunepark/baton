# Changelog

## [0.5.0](https://github.com/sjunepark/baton/compare/v0.4.4...v0.5.0) (2026-07-12)


### ⚠ BREAKING CHANGES

* remove baton complete and its local completion ledger, and replace repository file reconciliation plan v1 with v2. Unattended automation should gate work on repositorySnapshot v1 outcome=actionable.
* remove baton complete and its local completion ledger, and replace repository file reconciliation plan v1 with v2. Unattended automation should gate work on repositorySnapshot v1 outcome=actionable.
* remove baton complete and its local completion ledger, and replace repository file reconciliation plan v1 with v2. Unattended automation should gate work on repositorySnapshot v1 outcome=actionable.

### Features

* redesign repository orchestration ([e6511e8](https://github.com/sjunepark/baton/commit/e6511e87f7665bc20aa5abcf59c7b4bbefefd089))


### Documentation

* align Baton architecture and agent guidance ([ed553c3](https://github.com/sjunepark/baton/commit/ed553c33a3da619f7e84ecde0e8a10e3fae3c247))
* publish v0.5.0 contract migration ([ffe5e40](https://github.com/sjunepark/baton/commit/ffe5e4046d8cd890b6668d7a982d199ce8afadf8))

## [0.4.4](https://github.com/sjunepark/baton/compare/v0.4.3...v0.4.4) (2026-07-01)


### Features

* add Baton update skill command ([df1a0fa](https://github.com/sjunepark/baton/commit/df1a0fac2d70a9d68fb1cd70bf93781e0a352c3f))

## [0.4.3](https://github.com/sjunepark/baton/compare/v0.4.2...v0.4.3) (2026-07-01)


### Features

* add Baton adopter update guidance ([41badd1](https://github.com/sjunepark/baton/commit/41badd1090b69020312e44d6b18e95b7aa9788c0))

## [0.4.2](https://github.com/sjunepark/baton/compare/v0.4.1...v0.4.2) (2026-07-01)


### Features

* polish CLI version output ([4ab677c](https://github.com/sjunepark/baton/commit/4ab677c9f15b046bb34ef3d0893a191261d9a5ab))

## [0.4.1](https://github.com/sjunepark/baton/compare/v0.4.0...v0.4.1) (2026-07-01)


### Features

* add issue priority labels ([52d2465](https://github.com/sjunepark/baton/commit/52d24653023ccb0f556b228073f88633c35bbafd))

## [0.4.0](https://github.com/sjunepark/baton/compare/v0.3.0...v0.4.0) (2026-07-01)


### ⚠ BREAKING CHANGES

* revise next candidate selection contract

### Features

* revise next candidate selection contract ([2adc1eb](https://github.com/sjunepark/baton/commit/2adc1eb4438f24d4342b3fe54cf4a32c10f07f56))

## [0.3.0](https://github.com/sjunepark/baton/compare/v0.2.1...v0.3.0) (2026-06-30)


### ⚠ BREAKING CHANGES

* remove Baton-managed worktree leasing

### Features

* remove Baton-managed worktree leasing ([c15fd2c](https://github.com/sjunepark/baton/commit/c15fd2c0c11e450e900e31573cf60115f58d9542))

## [0.2.1](https://github.com/sjunepark/baton/compare/v0.2.0...v0.2.1) (2026-06-30)


### Features

* rename issue quality gate label to needs-info ([a25dfdd](https://github.com/sjunepark/baton/commit/a25dfddfdf0c888043817bb8a8879ca557d634ad))

## [0.2.0](https://github.com/sjunepark/baton/compare/v0.1.5...v0.2.0) (2026-06-30)


### ⚠ BREAKING CHANGES

* baton next now emits schemaVersion 2 nextCandidates with candidates[] instead of nextAction with singular pr or issue fields.

### Features

* adopt release please ([0f4c6fd](https://github.com/sjunepark/baton/commit/0f4c6fdc0ca99eb1f257b802620cd47773133520))
* **docs:** add lite (light) theme toggle to overview and tutorial ([c2d021d](https://github.com/sjunepark/baton/commit/c2d021dc61622ca308b3a5eaf4bae864ac44e420))
* return next candidate sets ([7e2911b](https://github.com/sjunepark/baton/commit/7e2911b2fb0d95716c387ee1a2d29a8f059faab6))


### Bug Fixes

* surface missing staging branch setup ([a4987af](https://github.com/sjunepark/baton/commit/a4987af7620338f7bd672d711ee6d2b6d0de0dfa))

## 0.1.5 (2026-06-28)

- Latest manual release before Release Please adoption.
