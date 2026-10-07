# Modbus addressing: a practical guide

This guide explains how Modbus addresses work and how to recognize the mistakes that make a reading come out wrong. It does not depend on modtop; the examples only use it to illustrate.

## The four tables

A Modbus device exposes its data in four tables. Each table is read with its own function code.

| Table | Contents | Read with | Item size | Modicon prefix |
|---|---|---|---|---|
| Coils | on/off outputs | FC01 | 1 bit | `0` |
| Discrete inputs | on/off inputs | FC02 | 1 bit | `1` |
| Input registers | read-only measurements | FC04 | 16 bits | `3` |
| Holding registers | values and settings | FC03 | 16 bits | `4` |

The tables are separate: input register 1 and holding register 1 are two different things, even though both are "register 1".

## The three notations

Manuals write the same address in three different ways. Take the first holding register:

| Notation | Example | Meaning |
|---|---|---|
| Base 0 (PDU address) | `0` | the number that actually travels in the frame, 0–65535 |
| Base 1 | `1` | the data model numbering, 1–65536 |
| Modicon | `40001` or `400001` | table prefix + base 1 number |

The conversions:

- `wire address = base 1 number − 1`
- **Modicon, 5 digits:** prefix + base 1 number on 4 digits (`0001`–`9999`). `40001` is holding register 1; `30010` is input register 10. It covers wire addresses 0–9998.
- **Modicon, 6 digits:** prefix + base 1 number on 5 digits (`00001`–`65536`). `400001` is holding register 1; `465536` is holding register 65536. It covers the whole range.
- The prefix is **never** sent on the wire. It only tells which table, and therefore which function code, to use.

Examples:

| Manual says | Table | Function | On the wire |
|---|---|---|---|
| `40001` | holding register | FC03 | 0 |
| `40101` | holding register | FC03 | 100 |
| `30010` | input register | FC04 | 9 |
| `10005` | discrete input | FC02 | 4 |
| `00001` | coil | FC01 | 0 |

When a manual lists plain numbers ("register 100"), it must also say whether they start at 0 or 1, and which table they belong to. If it does not, compare a known value (a serial number, a nominal voltage) to find out.

## The four classic mistakes

### 1. Off-by-one

The manual says `40101`, but the tool is asked for wire address 101 instead of 100. Every value appears shifted by one register.

How to recognize it: list a range, not a single register. The expected value shows up in the neighbor above or below. modtop lists consecutive registers for exactly this reason, and shows the translation of the selected address (`40101 → Holding register #101 → FC03 → on the wire: 100 (0x0064)`).

### 2. Wrong table

Reading with FC03 (holding registers) something that lives in the input registers (FC04), or the opposite. The device either answers with exception 02 (illegal data address) or returns unrelated values.

How to recognize it: `3xxxx` addresses are input registers, `4xxxx` are holding registers. If the manual uses plain numbers, check which table it means.

### 3. Wrong word order

A 32-bit value occupies two registers, R0 (lower address) and R1. Each register is sent with its most significant byte first. Name the bytes:

```
R0 = A B      R1 = C D
```

Devices assemble the four bytes in different orders before reading the value:

| Order | Bytes assembled |
|---|---|
| ABCD | A B C D |
| CDAB | C D A B |
| BADC | B A D C |
| DCBA | D C B A |

Example: R0 = `0x8000`, R1 = `0x44A2`.

| Order | Bytes | As float32 |
|---|---|---|
| ABCD | `80 00 44 A2` | −2.4620814e−41 |
| CDAB | `44 A2 80 00` | **1300.0** |
| BADC | `00 80 A2 44` | 1.1813153e−38 |
| DCBA | `A2 44 00 80` | −2.6563218e−18 |

Only CDAB gives a value with physical meaning (1300.0, say watts). A float32 that is absurdly small, absurdly large or negative where it cannot be is often just the wrong order. modtop shows the four interpretations side by side so the right one stands out.

### 4. Off-by-one in disguise

With 32-bit types, being one register off means the pair is made of the second half of one value and the first half of the next. The result is an absurd number in every byte order, which looks like a word order problem.

How to recognize it: if none of the four orders gives a sensible value, move one register up or down and try again before blaming the byte order.

## A note on block reads

Reading many registers in one request is efficient, but some devices answer a block that covers nonexistent registers with zeros instead of an error. If some values are 0 where you expected data, read the registers one at a time (in modtop, `--single`).

## Sources

The primary references are published by the Modbus Organization at [modbus.org](https://modbus.org):

- *Modbus Application Protocol Specification* (function codes, data model, exceptions).
- *Modbus over Serial Line Specification and Implementation Guide* (RTU framing, timing, electrical interface).
