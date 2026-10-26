"""Compare static upper-room landmarks; reports geometry, never sends inputs."""
import argparse
import json
from pathlib import Path

import cv2
import numpy as np


def room_offset(reference, current):
    if reference is None or current is None or reference.shape[:2] != (720,1280) or current.shape[:2] != (720,1280):
        raise ValueError('Expected two native 1280x720 frames')
    mask=np.zeros((720,1280),np.uint8);mask[145:235,300:1110]=255
    orb=cv2.ORB_create(nfeatures=1600,edgeThreshold=12,fastThreshold=10)
    ka,da=orb.detectAndCompute(cv2.cvtColor(reference,cv2.COLOR_BGR2GRAY),mask)
    kb,db=orb.detectAndCompute(cv2.cvtColor(current,cv2.COLOR_BGR2GRAY),mask)
    result=dict(usable=False,input_authorized=False)
    if da is None or db is None:return dict(result,reason='No architectural descriptors')
    pairs=cv2.BFMatcher(cv2.NORM_HAMMING).knnMatch(da,db,k=2)
    good=[m for pair in pairs if len(pair)==2 for m,n in [pair] if m.distance<.7*n.distance and m.distance<=45]
    if len(good)<10:return dict(result,reason='Insufficient unique matches',matches=len(good))
    a=np.float32([ka[m.queryIdx].pt for m in good]);b=np.float32([kb[m.trainIdx].pt for m in good])
    transform,inside=cv2.estimateAffinePartial2D(a,b,method=cv2.RANSAC,ransacReprojThreshold=2,maxIters=2000)
    if transform is None:return dict(result,reason='No consistent local transform')
    keep=inside[:,0].astype(bool);aa=a[keep];bb=b[keep]
    if len(aa)<10:return dict(result,reason='Insufficient inliers')
    pred=aa@transform[:,:2].T+transform[:,2];residual=float(np.median(np.linalg.norm(pred-bb,axis=1)))
    scale=float(np.hypot(transform[0,0],transform[1,0]));angle=float(np.degrees(np.arctan2(transform[1,0],transform[0,0])))
    offset=transform@np.array([640,190,1])-np.array([640,190]);span=float(np.ptp(aa[:,0]))
    usable=span>=160 and residual<=2 and .95<=scale<=1.05 and abs(angle)<=.8
    return dict(result,usable=bool(usable),dx=float(offset[0]),dy=float(offset[1]),scale=scale,angle_degrees=angle,inliers=len(aa),horizontal_span=span,residual_pixels=residual)


def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('reference',type=Path);parser.add_argument('current',type=Path);a=parser.parse_args()
    print(json.dumps(room_offset(cv2.imread(str(a.reference)),cv2.imread(str(a.current)))))


if __name__=='__main__':main()
