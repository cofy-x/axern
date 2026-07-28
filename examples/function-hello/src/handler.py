def init(context):
    return {"initialized": True, "function": context.function_name}


def hello(event, context):
    name = event.get("name", "world")
    greeting = context.env.get("GREETING", "hello")
    return {
        "message": f"{greeting} {name}",
        "request_id": context.request_id,
        "state": context.state,
    }
