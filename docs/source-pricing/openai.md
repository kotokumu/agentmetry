# OpenAI API Pricing Snapshot

## 1. Snapshot metadata

| Field | Value |
|---|---|
| Provider | OpenAI |
| Retrieved | `2026-09-09T03:39:43+09:00` |
| Evidence boundary | Official model pages listed below |
| Unit | USD per 1 million tokens |

This document freezes the pricing facts used by Agentmetry's initial Codex
cost estimates. The effective boundary used by the product is a separate,
conservative product decision; it is not asserted to be the provider's
historical price start date.

## 2. Source references

The SHA-256 values cover the decoded HTML response body returned by `curl -L`
at the retrieval time.

| ID | Official URL | Content-Type | SHA-256 |
|---|---|---|---|
| `OPENAI-GPT-6-ASTRA` | [GPT-6 Astra](https://developers.openai.com/api/docs/models/gpt-6-astra) | `text/html` | `318b9aa8451809f2544539e36435ad348f1aa8f5dc97c54bce65538a18f06357` |
| `OPENAI-GPT-5.6-SOL` | [GPT-5.6 Sol](https://developers.openai.com/api/docs/models/gpt-5.6-sol) | `text/html` | `a117cc7b414fe01312c9809ff6ad6e91287eb75f39ed9df7feb22abee60f944a` |
| `OPENAI-GPT-5.6-TERRA` | [GPT-5.6 Terra](https://developers.openai.com/api/docs/models/gpt-5.6-terra) | `text/html` | `258aaa022106895554fff8abbea3e187a7011b27935d0fecaab5646ca623a204` |
| `OPENAI-GPT-5.6-LUNA` | [GPT-5.6 Luna](https://developers.openai.com/api/docs/models/gpt-5.6-luna) | `text/html` | `6a6224480e50a5fd2c1b6658af2888774f729472e20f1edf403ae1eb30a4be9f` |

## 3. Extracted pricing facts

| Exact model | Input | Cached input | Output | Source |
|---|---:|---:|---:|---|
| `gpt-6-astra` | 10.00 | 1.00 | 50.00 | `OPENAI-GPT-6-ASTRA` |
| `gpt-5.6-sol` | 4.00 | 0.40 | 20.00 | `OPENAI-GPT-5.6-SOL` |
| `gpt-5.6-terra` | 2.00 | 0.20 | 12.00 | `OPENAI-GPT-5.6-TERRA` |
| `gpt-5.6-luna` | 0.20 | 0.02 | 1.20 | `OPENAI-GPT-5.6-LUNA` |

The pages state that cache writes cost 1.25 times the uncached input rate.
Agentmetry therefore derives cache-write rates without further rounding:

| Exact model | Source input | Multiplier | Derived cache write |
|---|---:|---:|---:|
| `gpt-6-astra` | 10.00 | 1.25 | 12.50 |
| `gpt-5.6-sol` | 4.00 | 1.25 | 5.00 |
| `gpt-5.6-terra` | 2.00 | 1.25 | 2.50 |
| `gpt-5.6-luna` | 0.20 | 1.25 | 0.25 |

The pages also state that requests above 272K input tokens use different
multipliers. Those requests, Batch, Flex, Fast, regional processing, and
tool-specific charges are excluded from the initial rate rows.

## 4. Seed manifest golden

The seed uses provider `openai`, mode `standard`, condition
`max_input_tokens_inclusive=272000`, effective-from
`2026-09-08T15:00:00Z`, and retrieved-at
`2026-09-08T18:39:43Z`. Rates below are integer micro-USD per 1 million
tokens. Tests copy these literals; they do not calculate expected values from
the production manifest.

The rate ID input starts with ASCII `agentmetry-rate-id-v1` and NUL. It then
encodes provider, model, mode, condition, and effective-from as five repetitions
of `uint32 big-endian UTF-8 byte length` followed by the UTF-8 bytes. The rate
ID is the lowercase hexadecimal SHA-256 of the resulting bytes.

| Exact model | Input | Cache read | Cache write | Output | Evidence ID | Rate ID |
|---|---:|---:|---:|---:|---|---|
| `gpt-6-astra` | 10000000 | 1000000 | 12500000 | 50000000 | `OPENAI-GPT-6-ASTRA` | `82329dc990b04cc04ecd5b02e131caffc00429dd336bda152cc575f697dc0228` |
| `gpt-5.6-sol` | 4000000 | 400000 | 5000000 | 20000000 | `OPENAI-GPT-5.6-SOL` | `735bc3af0d3c62a1ecc8e9969c6394f88ed1bf7aefde3762fd7f55f4839d3022` |
| `gpt-5.6` | 4000000 | 400000 | 5000000 | 20000000 | `OPENAI-GPT-5.6-SOL` | `6e295bd9b21dee32098ab88aa4a87fd4edc7f421ba872fb0f238d72e07099d76` |
| `gpt-5.6-terra` | 2000000 | 200000 | 2500000 | 12000000 | `OPENAI-GPT-5.6-TERRA` | `18fdc8fdd33fbdc68adaaba385e67a5096690bfff500be5ae659f2202157f11e` |
| `gpt-5.6-luna` | 200000 | 20000 | 250000 | 1200000 | `OPENAI-GPT-5.6-LUNA` | `1f7c49e8da4d736db51d653ba9c00a31879f0c0527a4cd320cc952033fd519b8` |

For a literal encoding oracle, the `gpt-6-astra` canonical bytes are:

```text
6167656e746d657472792d726174652d69642d763100000000066f70656e61690000000b6770742d362d6173747261000000087374616e64617264000000216d61785f696e7075745f746f6b656e735f696e636c75736976653d32373230303000000014323032362d30392d30385431353a30303a30305a
```

The fixed-point calculation golden uses `gpt-6-astra`, total input 100,
cache read 20, cache write 10, output 50, and reasoning 30. Uncached input is
70. Per-component half-up results are 700, 20, 125, and 2500 micro-USD, for a
total of 3345 micro-USD. Reasoning is an output breakdown and adds zero.

## 5. Accepted product assumptions

- `gpt-5.6` is accepted as an exact alias of `gpt-5.6-sol` for the initial
  Agentmetry seed. It receives its own exact-match row with the same rates;
  unknown model names are never matched by prefix or fuzzy comparison.
- Initial rows begin at `2026-09-09T00:00:00+09:00`. Calls before that boundary
  remain unpriced because this snapshot does not prove earlier prices.
- The result is an API-equivalent estimate, not an invoice or subscription-plan
  allocation.
