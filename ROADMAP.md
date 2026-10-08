# Roadmap

This roadmap covers October 2026 to October 2027. It states intent, not
promises: it helps users and contributors see where modtop is going and
where it is not going. Every feature still has to pass the criteria in
[GOVERNANCE.md](GOVERNANCE.md), starting with a real field case.

## Now: 0.1.x

- Validate 0.1 on real hardware (TCP and RTU gateways, meters, USB-RS485
  adapters) and fix what the field finds.
- Keep the documentation in step with the behavior.

## Next: 0.2, session profiles

The theme of 0.2 is saving and loading a session description (target,
range, types and byte orders per register) in a file, so a known device
does not have to be set up again by hand. Profiles will be a new contract
and will be documented as such.

## Later, only with a real field case

Ideas are recorded in the "Ideas on record" section of
[CHANGELOG.md](CHANGELOG.md). None is planned until someone brings a
concrete case.

## Not planned

These are permanent non-goals:

- historian or long-term storage;
- alarms and notifications;
- automatic control;
- web dashboard or GUI;
- other protocols (IEC 104, OPC UA, MQTT, and so on).

Writing to devices stays out of scope except in the diagnostic form described
in [GOVERNANCE.md](GOVERNANCE.md): isolated, explicit, confirmed and logged.
