---
sidebar_position: 3
---

# Readiness

Not all resources are ready immediately after creation. A Deployment might exist but have zero available replicas, or a LoadBalancer Service might not have an external IP yet. If dependent resources try to use values that don't exist yet, they'll fail or get invalid data.

kro provides the `readyWhen` field to define when a resource is considered ready. When you add `readyWhen` to a resource, kro waits for all conditions to be true before proceeding with dependent resources.

## Basic Example

Here's a simple example where a database must be fully ready before the application deployment is created:

```yaml
resources:
  - id: database
    template:
      apiVersion: database.example.com/v1
      kind: PostgreSQL
      metadata:
        name: ${schema.spec.name}-db
      spec:
        version: "15"
    readyWhen:
      - ${database.status.conditions.exists(c, c.type == "Ready" && c.status == "True")}
      - ${database.status.?endpoint != ""}

  - id: app
    template:
      apiVersion: apps/v1
      kind: Deployment
      metadata:
        name: ${schema.spec.name}
      spec:
        template:
          spec:
            containers:
              - name: app
                image: ${schema.spec.image}
                env:
                  - name: DATABASE_HOST
                    value: ${database.status.endpoint}
```

The `app` deployment won't be created until:
1. The database resource exists **AND**
2. The database has a Ready condition with status True **AND**
3. The database has an endpoint in its status

This ensures `${database.status.endpoint}` has a valid value when the app is created.

## How readyWhen Works

`readyWhen` is a list of CEL expressions that control when a resource is considered ready:

- **Without `readyWhen`**: Resources are ready as soon as they exist in the cluster
- **With `readyWhen`**: Resources are created but remain in a waiting state until all conditions are true
- If **all** expressions evaluate to `true`, the resource is marked ready
- If **any** expression evaluates to `false`, the resource continues waiting
- Each expression must evaluate to a **boolean** value (`true` or `false`)
- **Dependent resources wait** until all their dependencies are ready

## What You Can Reference

`readyWhen` expressions can only reference the resource itself (by its `id`):

```yaml
# ✓ Valid - references the resource itself and returns boolean
- id: deployment
  readyWhen:
    - ${deployment.status.availableReplicas > 0}
    - ${deployment.status.conditions.exists(c, c.type == "Available" && c.status == "True")}
```

```yaml
# ✗ Invalid - cannot reference other resources or schema
- id: deployment
  readyWhen:
    - ${service.status.loadBalancer.ingress.size() > 0}  # Can't reference other resources
    - ${schema.spec.replicas > 3}  # Can't reference schema
    - ${deployment.status.availableReplicas}  # Must return boolean
```

kro validates `readyWhen` expressions when you create the ResourceGraphDefinition, ensuring they reference valid fields and return boolean values.

:::important
`readyWhen` determines when **this specific resource** is ready. It can't depend on other resources' states—that's handled automatically by the dependency graph when you reference other resources in your templates. This keeps readiness conditions local, deterministic, and easy to debug.
:::

## Dependencies and Readiness

When a resource has a `readyWhen` condition, **all resources that depend on it must wait** until it's ready.

kro processes resources in the correct order based on references. If a resource references another resource's status field, kro:
1. Creates the referenced resource first
2. Waits for its `readyWhen` conditions (if any) to be satisfied
3. Only then creates the dependent resource with the correct status values

This ensures your resources always have valid data and prevents race conditions.

## The Optional Operator (?)

Status fields often don't exist immediately after resource creation. Use the optional operator `?` to safely access fields that might be missing:

```yaml
readyWhen:
  # ✓ Safe: returns false if endpoint doesn't exist yet
  - ${database.status.?endpoint != ""}

  # ✓ Safe: returns false if field is missing
  - ${service.status.?loadBalancer.?ingress.size() > 0}

  # ✗ Unsafe: will error if endpoint doesn't exist
  - ${database.status.endpoint != ""}
```

The `?` operator returns `null` if the field doesn't exist instead of causing an error. This is essential for checking status fields in `readyWhen` conditions.

## Next Steps

- **[Dependencies & Ordering](../04-dependencies-ordering.md)** - Understand how kro determines resource creation order
- **[Conditional Resources](./02-conditional-creation.md)** - Control whether resources are created
- **[CEL Expressions](../03-cel-expressions.md)** - Master expression syntax for readiness conditions
