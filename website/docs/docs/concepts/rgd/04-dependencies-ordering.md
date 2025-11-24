---
sidebar_position: 4
---

# Graph Inference

kro automatically infers dependencies from CEL expressions. You don't specify the order - you describe relationships, and kro figures out the rest.

## How It Works

When you reference one resource from another using a CEL expression, you create a dependency:

```yaml
resources:
  - id: configmap
    template:
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: app-config
      data:
        DATABASE_URL: ${schema.spec.dbUrl}

  - id: deployment
    template:
      apiVersion: apps/v1
      kind: Deployment
      spec:
        containers:
          - env:
              - name: DATABASE_URL
                value: ${configmap.data.DATABASE_URL}
```

The expression `${configmap.data.DATABASE_URL}` creates a dependency: `deployment` depends on `configmap`. kro will create the configmap first, wait for the expression to be resolvable, then create the deployment.

## Dependency Graph (DAG)

kro builds a Directed Acyclic Graph (DAG) where:
- **Nodes** are resources
- **Edges** are dependencies
- **Directed** means dependencies have direction (A depends on B)
- **Acyclic** means no circular dependencies

Simple chain:
```
schema → configmap → deployment → service → ingress
```

Multiple dependencies:
```
        schema
          ↓
    ┌─────┴─────┐
    ↓           ↓
database     cache
    └─────┬─────┘
          ↓
         app
```

## Topological Order

kro computes a topological order - the sequence resources can be processed such that all dependencies are satisfied.

**Creation:** Resources created in topological order
**Deletion:** Resources deleted in reverse order

View the computed order:
```bash
kubectl get rgd my-app -o jsonpath='{.status.topologicalOrder}'
```

Example output:
```yaml
status:
  topologicalOrder:
    - configmap
    - deployment
    - service
```

## Circular Dependencies

Circular dependencies are not allowed and will cause validation to fail:

```yaml
# ✗ This will fail
resources:
  - id: serviceA
    template:
      spec:
        targetPort: ${serviceB.spec.port}  # A → B

  - id: serviceB
    template:
      spec:
        targetPort: ${serviceA.spec.port}  # B → A (circular!)
```

**Fix:** Break the cycle by using `schema.spec` instead:
```yaml
resources:
  - id: serviceA
    template:
      spec:
        targetPort: ${schema.spec.portA}  # Use schema

  - id: serviceB
    template:
      spec:
        targetPort: ${serviceA.spec.port}  # This is fine
```

## What Happens at Runtime

When kro reconciles an instance:

1. **Evaluate static expressions** - Expressions referencing only `schema.spec` are evaluated once
2. **Process in topological order** - For each resource:
   - Wait for all dependency expressions to be resolvable
   - Create or update the resource
   - Move to next resource in order
3. **Delete in reverse order** - During deletion, process resources backwards

kro waits for CEL expressions to be **resolvable** before proceeding. This means the referenced resource exists and has the field being accessed.

## Next Steps

- **[Readiness](./02-resource-definitions/03-readiness.md)** - Control when resources are considered ready with `readyWhen`
- **[CEL Expressions](./03-cel-expressions.md)** - Learn more about writing expressions
- **[Resource Basics](./02-resource-definitions/01-resource-basics.md)** - Learn about resource templates
