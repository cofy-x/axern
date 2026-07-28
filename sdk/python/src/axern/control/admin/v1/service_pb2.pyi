from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class PurgeServiceRequest(_message.Message):
    __slots__ = ("service_id", "operator_reason")
    SERVICE_ID_FIELD_NUMBER: _ClassVar[int]
    OPERATOR_REASON_FIELD_NUMBER: _ClassVar[int]
    service_id: str
    operator_reason: str
    def __init__(self, service_id: _Optional[str] = ..., operator_reason: _Optional[str] = ...) -> None: ...

class PurgeServiceResponse(_message.Message):
    __slots__ = ("service_id",)
    SERVICE_ID_FIELD_NUMBER: _ClassVar[int]
    service_id: str
    def __init__(self, service_id: _Optional[str] = ...) -> None: ...
