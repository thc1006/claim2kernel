#!/usr/bin/env python3
"""CPU RZF benchmark that emits measured evidence, never fabricated GPU data."""
from __future__ import annotations
import argparse, json, platform, statistics, time
from pathlib import Path
import numpy as np
from rzf_reference import metrics, rzf_precoder


def bench(batch:int, users:int, antennas:int, reps:int, warmup:int, seed:int)->dict:
    rng=np.random.default_rng(seed);h=(rng.normal(size=(batch,users,antennas))+1j*rng.normal(size=(batch,users,antennas)))/np.sqrt(2)
    ref=rzf_precoder(h,.1,np.complex128)
    for _ in range(warmup):rzf_precoder(h,.1,np.complex64)
    samples=[];candidate=None
    for _ in range(reps):
        start=time.perf_counter_ns();candidate=rzf_precoder(h,.1,np.complex64);samples.append((time.perf_counter_ns()-start)/1000)
    assert candidate is not None
    return {"batch":batch,"users":users,"antennas":antennas,"precision":"complex64","repetitions":reps,"latencyUS":{"min":min(samples),"p50":float(np.quantile(samples,.5)),"p95":float(np.quantile(samples,.95)),"p99":float(np.quantile(samples,.99)),"max":max(samples),"mean":statistics.fmean(samples)},"numerical":metrics(ref,candidate,h)}

def main()->None:
 p=argparse.ArgumentParser();p.add_argument('--smoke',action='store_true');p.add_argument('--out',required=True);p.add_argument('--reps',type=int,default=50);a=p.parse_args();shapes=[(1,4,16),(2,8,32)] if a.smoke else [(1,4,32),(1,8,64),(4,8,64),(4,16,128)];reps=10 if a.smoke else a.reps
 result={"evidenceType":"measured-cpu-reference","notGPU":True,"host":{"python":platform.python_version(),"platform":platform.platform(),"numpy":np.__version__},"cases":[bench(b,u,n,reps,3,100+i) for i,(b,u,n) in enumerate(shapes)]};out=Path(a.out);out.parent.mkdir(parents=True,exist_ok=True);out.write_text(json.dumps(result,indent=2,allow_nan=False)+'\n');print(json.dumps({"written":str(out),"cases":len(shapes)}))
if __name__=='__main__':main()
