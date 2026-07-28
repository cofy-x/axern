# Function Hello Golden Example

This directory is the product-shape example for Axern Function. It defines the
source layout, manifest, payload, and Python handler contract.

## Shape

```text
function-hello/
|-- function.yaml
|-- payload.json
`-- src/
    `-- handler.py
```

## CLI Flow

```bash
# Deploy the function (bundles src/, uploads, and creates the function)
axern fn deploy --file examples/function-hello/function.yaml

# Wait for the function to become ready
axern fn deploy --file examples/function-hello/function.yaml --wait

# Invoke with a JSON payload file
axern fn invoke hello --payload-file examples/function-hello/payload.json

# Invoke with inline JSON
axern fn invoke hello -d '{"name": "axern"}'

# Asynchronous invocation (returns immediately)
axern fn invoke hello -d '{"name": "async"}' --async

# Inspect function state
axern fn get hello
axern fn list

# View invocation details and history
axern fn invocation get <invocation_id>
axern fn invocation list --namespace default hello
axern fn invocation events <invocation_id>

# Clean up
axern fn delete hello
```

## SDK Flow

```python
from axern_sdk import AxernClient, Function

client = AxernClient.from_env()
fn = Function.from_file(client, "examples/function-hello/function.yaml")

fn.deploy(wait_ready=True)
result = fn.invoke({"name": "axern"})
print(result.value)   # handler return value
print(result.status)  # "succeeded"
```
