# toy-stack

Self-contained Pulumi demo for reeve. Two projects × two stacks each,
using only the `random` provider so no cloud credentials are required.

## Layout

```
examples/toy-stack/
├── .reeve/
│   ├── shared.yaml
│   └── pulumi.yaml
└── projects/
    ├── random-name/
    │   ├── Pulumi.yaml
    │   ├── Pulumi.dev.yaml
    │   ├── Pulumi.prod.yaml
    │   ├── index.ts
    │   ├── package.json
    │   └── tsconfig.json
    └── random-secret/
        ├── Pulumi.yaml
        ├── Pulumi.dev.yaml
        ├── index.ts
        ├── package.json
        └── tsconfig.json
```

## Try it

No external dependencies needed for enumeration and lint:

```
cd examples/toy-stack
reeve lint
reeve stacks
```

Full preview requires `pulumi` CLI and `node` + `npm install` in each
project:

```
cd projects/random-name && npm install && pulumi login file://. && pulumi stack init dev
cd ../..
reeve plan-run --root . --sha demo
```
