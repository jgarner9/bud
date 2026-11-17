"""bud-signal: Signal approvals answered by reaction or code (Suite D30). bud sync installs it."""


def register(ctx):
    from .adapter import register as register_signal

    register_signal(ctx)
