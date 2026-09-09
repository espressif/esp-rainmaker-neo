# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""The preconditions the SDK checks before it runs anything, and how it reports an unmet one.

The SDK states the condition; the caller words the remedy. A phrase here names what is absent,
never a Python method: the same Device drives the REPL, `morpheus device`, the simulators and the
itests, and none of them share a vocabulary.

A guarded class declares the subject its phrases are about, and where its trace goes, as
`not_ready_subject` and `not_ready_log`. Device and User share the tokens they have in common.

An unmet precondition raises NotReadyError, so a method that has no falsy value to return cannot
refuse a call in silence. A method whose callers read a return value says so — `blocked=False` or
`blocked=None` — and keeps the contract it already had.

Either way the condition lands on `not_ready` as a token the caller can match on, which is
how a caller says more than "it failed" without parsing prose.
"""

import functools

# Precondition token -> the attribute that carries it, and the phrase naming what is absent.
_PRECONDITIONS = {
    'mqtt': ('mqtt_connection', 'MQTT connection'),
    'shadow': ('shadow_client', 'shadow connection'),
    'group_info': ('group_id', 'group info'),
    'node_key': ('node_key', 'private key'),
    'node_cert': ('node_cert', 'certificate'),
    'node_id': ('node_thing_name', 'thing name'),
}


class NotReady:
    """What an operation blocked on.

    @note A record, not an exception: `need` is the contract a caller matches on, and the string
    form is a lowercase clause the caller joins to its own words.
    """

    def __init__(self, need, subject):
        self.need = need
        self.subject = subject

    def __str__(self):
        return f"the {self.subject} has no {_PRECONDITIONS[self.need][1]}"


class NotReadyError(Exception):
    """An unmet precondition, raised because the operation has no falsy value to report it with.

    @note `need` is the contract a caller matches on, as it is on NotReady.
    """

    def __init__(self, blocked):
        self.need = blocked.need
        super().__init__(str(blocked))


def block(subject, need):
    """Record `need` as what blocked the current call on `subject`, and trace it."""
    subject.not_ready = NotReady(need, subject.not_ready_subject)
    subject.not_ready_log(f"Not ready: {subject.not_ready}")
    return subject.not_ready


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
