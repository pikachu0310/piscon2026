# Score and capacity frontiers

## Score champion

- Status: B0 validated
- Start commit: `e70329ffe14967d1959d528b056c31d1dd53a9c9`
- Topology declaration: s1=Nginx, s2=MariaDB, s3=Go App
- Benchmark ID / score / validity: `8c9bab42-1521-421b-8be1-23e77a008fea`,
  **134,310**, PASSED, deduction 0
- Artifact: `20260720T092734.057516Z-s1-a948bf`
- Deployed app source provenance: `f33da52b55f9348d24eb9bbd488033428b3bdfad`
- App binary SHA-256: `3dd03256c0316d7de0cc32102a618bdb898218e1afed95baab0edd7e0a9145f2`
- s1 Nginx site SHA-256: `9ad5ce44a6e0417c104b8db6605a08c64a2a9ec6debd59a0b30a818b432e81af`
- s2 MariaDB tuning SHA-256: `b7462f1f41615fa7a733871d2aed3969973708e02497d0f5526e8e4e10ea31af`
- s3 topology SHA-256: `d399071c8ed066d29ce9e942d3a604cd823b6c1151bb885c89c76fe4987fbc24`

## Capacity frontier

None yet. A candidate belongs here only when post-start evidence shows increased
valid offered work, lower unit cost, or a causal downstream bottleneck shift.

## Decision rule

The score champion is always restorable and is the final-mode candidate. A
capacity frontier is kept on its own commit/config while up to three downstream
experiments (five for a structural change with at least 20% more valid work)
attempt to convert capacity into official score.
