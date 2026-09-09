# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""The preconditions the SDK checks before it runs anything, and how it reports an unmet one.

The SDK states the condition; the caller words the remedy. A phrase here names what is absent,
never a Python method: the same Device drives the REPL, `morpheus device`, the simulators and the
itests, and none of them share a vocabulary.

An unmet precondition raises NotReadyError, so a method that has no falsy value to return cannot
refuse a call in silence. A method whose callers read a return value says so — `blocked=False` or
`blocked=None` — and keeps the contract it already had.

Either way the condition lands on `device.not_ready` as a token the caller can match on, which is
how a caller says more than "it failed" without parsing prose.
"""

import functools

# Precondition token -> the Device attribute that carries it, and the phrase naming what is absent.
_PRECONDITIONS = {
    'mqtt': ('mqtt_connection', 'MQTT connection'),
    'shadow': ('shadow_client', 'shadow connection'),
    'group_info': ('group_id', 'group info'),
    'node_key': ('node_key', 'private key'),
    'node_cert': ('node_cert', 'certificate'),
    'node_id': ('node_thing_name', 'thing name'),
}


class NotReady:
    """What a device operation blocked on.

    @note A record, not an exception: `need` is the contract a caller matches on, and the string
    form is a lowercase noun phrase the caller joins to its own words.
    """

    def __init__(self, need):
        self.need = need

    def __str__(self):
        return f"the node has no {_PRECONDITIONS[self.need][1]}"


class NotReadyError(Exception):
    """An unmet precondition, raised because the operation has no falsy value to report it with.

    @note `need` is the contract a caller matches on, as it is on NotReady.
    """

    def __init__(self, blocked):
        self.need = blocked.need
        super().__init__(str(blocked))


def block(device, need):
    """Record `need` as what blocked the current call on `device`, and trace it."""
    from .device import device_log  # deferred: device imports this module
    device.not_ready = NotReady(need)
    device_log(f"Not ready: {device.not_ready}")
    return device.not_ready


def requires(*needs, blocked=NotReadyError):
    """Refuse to run the method when a precondition is unmet.

    @note `blocked` says how the refusal reads: an exception class to raise, which is the default
    so that nothing refuses a call silently, or the value the method already returned on that path
    — False or None — which a method whose callers read a return value must state.
    """
    for need in needs:
        if need not in _PRECONDITIONS:
            raise KeyError(f"unknown precondition: {need}")
    raises = isinstance(blocked, type) and issubclass(blocked, BaseException)

    def decorate(method):
        @functools.wraps(method)
        def guarded(self, *args, **kwargs):
            self.not_ready = None
            for need in needs:
                if not getattr(self, _PRECONDITIONS[need][0], None):
                    record = block(self, need)
                    if raises:
                        raise blocked(record)
                    return blocked
            return method(self, *args, **kwargs)
        return guarded
    return decorate
