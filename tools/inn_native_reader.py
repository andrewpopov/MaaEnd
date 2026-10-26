"""Persistent MaaFramework OCR for saved inn frames; this controller rejects inputs.

The live OCR player remains the sole input owner. Each image and token file stay
paired, and innframe consumes the actual tokens rather than an assumed layout.
"""
import json
import re
from pathlib import Path
import subprocess
import time

import cv2
import numpy as np
from maa.resource import Resource
from maa.tasker import Tasker
from maa.toolkit import Toolkit

from inn_saved_ocr import SavedImageController


class NativeInnReader:
    def __init__(self, root, output, first_image):
        self.root = Path(root)
        self.output = Path(output)
        self.output.mkdir(parents=True, exist_ok=True)
        if self.output.resolve().drive.upper() == "C:":
            raise ValueError("Keep OCR evidence off C:")
        Toolkit.init_option(self.output / "image-only-ocr-runtime")
        self.resource = Resource()
        if not self.resource.post_bundle(self.root / "install/resource").wait().succeeded:
            raise RuntimeError("Native OCR resource load failed")
        self.controller = SavedImageController(first_image)
        if not self.controller.post_connection().wait().succeeded:
            raise RuntimeError("Image-only OCR controller failed")
        self.tasker = Tasker()
        if not self.tasker.bind(self.resource, self.controller):
            raise RuntimeError("Image-only OCR binding failed")

    def inspect(self, image, roi=None, native_report=None, target_seat=None):
        image = Path(image)
        frame = cv2.imread(str(image))
        if frame is None or frame.shape[:2] != (720, 1280):
            raise ValueError("Expected a native 1280x720 saved frame")
        self.controller.image = frame
        started = time.monotonic()
        timing = {"ocr_seconds": 0.0, "ocr_crops": 0}
        def read(region, only_rec=False):
            node = {"recognition": "OCR", "roi": region, "expected": [".*"], "only_rec": only_rec,
                    "action": "DoNothing", "pre_delay": 0, "post_delay": 0, "rate_limit": 0, "timeout": 1}
            before = time.monotonic()
            job = self.tasker.post_task("InnImageRead", {"InnImageRead": node}).wait()
            detail = job.get()
            timing["ocr_seconds"] += time.monotonic()-before
            timing["ocr_crops"] += 1
            if not job.succeeded:
                return []
            if detail is None or not detail.nodes:
                raise RuntimeError("Native image-only OCR returned no detail")
            return detail.nodes[-1].recognition.raw_detail["all"]
        tokens = list(native_report["tokens"]) if native_report else read(roi or [0,0,1280,720])
        if native_report is None and roi is None:
            tokens += read([80,280,1200,180])
        tokens = list({(t["text"], tuple(t["box"])): t for t in tokens}.values())
        if target_seat:
            target_seat=str(target_seat)
            if target_seat not in physical_boards(tokens):
                for parent in self.output.parents:
                    template_file=parent/"feedback-digit-templates"/(target_seat+".png")
                    if not template_file.is_file():continue
                    best=match_board_templates(frame,template_file.parent,target_seat)
                    if best[0]>=(.9 if target_seat=="1" else .8):
                        for digit in read(best[1],True):
                            if digit["text"].strip()==target_seat and digit["score"]>=.95:
                                digit["plaque"]=True;tokens.append(digit)
                    break
            if target_seat in physical_boards(tokens) or template_file.is_file():
                report={"image":str(image),"assumed_layout":"","input_authorized":False,"tokens":tokens,"boards":physical_boards(tokens),"timing":timing}
                timing["total_seconds"]=time.monotonic()-started
                image.with_suffix(".board-"+target_seat+".json").write_text(json.dumps(report,indent=2)+"\n",encoding="utf8")
                return report
        # Detection can join a number board and its narrow wooden support as
        # "61"/"51". Recognize the upper digit independently; never strip text.
        verified_digits = []
        for token in tokens:
            x,y,w,h = token["box"]
            if (re.fullmatch(r"[1-6]1", token["text"].strip()) and token["score"] >= .7
                    and 180 <= x <= 1240 and 250 <= y <= 430
                    and 10 <= w <= 55 and 45 <= h <= 100):
                crop = [x, y+2, w, min(50, round(h*.55))]
                for digit in read(crop, True):
                    if digit["text"].strip() == token["text"].strip()[0] and digit["score"] >= .95:
                        verified_digits.append(digit)
        for token in tokens:
            x,y,w,h = token["box"]
            digit=token["text"].strip()
            if digit in tuple("23456") and token["score"]>=.95 and 180<=x<=1240 and 250<=y<=430 and h/w>1.7:
                for upper in read([x,y,w,max(16,round(h*.6))],True):
                    if upper["text"].strip()==digit and upper["score"]>=.95:
                        upper["plaque"]=True
                        verified_digits.append(upper)
        tokens += verified_digits
        for box in plaque_boxes(frame):
            for digit in read(box, True):
                if digit["text"].strip() in tuple("123456") and digit["score"] >= .95:
                    digit["plaque"] = True
                    tokens.append(digit)
        if not all(seat in physical_boards(tokens) for seat in tuple("123456")):
            for parent in self.output.parents:
                folder = parent / "feedback-digit-templates"
                if not folder.is_dir():
                    continue
                gray = cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY)
                area = gray[250:470,180:1280]
                for seat in tuple("123456"):
                    if seat in physical_boards(tokens):
                        continue
                    if not (folder / (seat + ".png")).is_file():continue
                    template = cv2.imread(str(folder / (seat + ".png")), cv2.IMREAD_GRAYSCALE)
                    if template is None:
                        continue
                    best = (0, None)
                    for k in range(12,37):
                        scale = k / 20
                        w,h = round(template.shape[1]*scale),round(template.shape[0]*scale)
                        if w < 10 or h < 18 or h > area.shape[0]:
                            continue
                        tpl = cv2.resize(template,(w,h))
                        scores = cv2.matchTemplate(area,tpl,cv2.TM_CCOEFF_NORMED)
                        _,score,_,point = cv2.minMaxLoc(scores)
                        if score > best[0]:
                            best = (score,[180+point[0],250+point[1],w,h])
                    if best[0] >= (.86 if seat == "1" else .7):
                        x,y,w,h = best[1]
                        colors = frame[y:y+h,x:x+w].astype("float32")
                        blue,green,red = colors[:,:,0],colors[:,:,1],colors[:,:,2]
                        brown = (red > green*1.2) & (green > blue*1.1) & (red < 210)
                        if brown.mean() < .25:
                            continue
                        for digit in read(best[1],True):
                            if digit["text"].strip() == seat and digit["score"] >= .95:
                                tokens.append(digit)
                break
        token_file = image.with_suffix(".tokens.json")
        token_file.write_text(json.dumps(tokens, ensure_ascii=False) + "\n", encoding="utf8")
        before = time.monotonic()
        if native_report:
            report = {"image":str(image),"assumed_layout":"","input_authorized":False,
                      "view":native_report["view"],"native_task_id":native_report["task_id"]}
        else:
            result = subprocess.run([str(self.root / ".cache/innframe.exe"),
                "--image",str(image),"--tokens",str(token_file),"--hud-only",
                "--data",str(self.root / "install/data/Innkeeping")],
                capture_output=True,text=True,check=True)
            report = json.loads(result.stdout)
        timing["hud_seconds"] = time.monotonic()-before
        timing["total_seconds"] = time.monotonic()-started
        report["timing"] = timing
        report["tokens"] = tokens
        report["boards"] = physical_boards(tokens)
        image.with_suffix(".read.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf8")
        return report


def physical_boards(tokens):
    """Keep high-confidence standalone digits in the measured furniture region."""
    result = {}
    plaque_ids = {t["text"].strip() for t in tokens if t.get("plaque")}
    for token in tokens:
        text = token["text"].strip()
        x, y, w, h = token["box"]
        if (text not in tuple("123456") or token["score"] < .95
                or not 270 <= x <= 1100 or not 250 <= y <= 430
                or not 8 <= w <= 55 or not 16 <= h <= 80):
            continue
        if text != "1" and h/w>1.8 and not token.get("plaque"):
            continue
        old = result.get(text)
        if old is None or token.get("plaque") or h > old[3] and text not in plaque_ids:
            result[text] = [x, y, w, h]
    return result


def plaque_boxes(frame):
    """Locate brown rectangular plaques, removing their narrower wooden supports."""
    blue,green,red = [frame[:,:,i].astype("float32") for i in range(3)]
    mask = ((red > green*1.25) & (green > blue*1.12) & (red > 70)
            & (red < 205) & (green > 35)).astype("uint8") * 255
    mask[:250,:] = 0
    mask[470:,:] = 0
    mask[:,:180] = 0
    mask = cv2.morphologyEx(mask,cv2.MORPH_CLOSE,np.ones((7,7),np.uint8))
    boxes = set()
    for width in (7,15,25):
        opened = cv2.morphologyEx(mask,cv2.MORPH_OPEN,np.ones((5,width),np.uint8))
        contours,_ = cv2.findContours(opened,cv2.RETR_EXTERNAL,cv2.CHAIN_APPROX_SIMPLE)
        for contour in contours:
            x,y,w,h = cv2.boundingRect(contour)
            if (8 <= w <= 85 and 18 <= h <= 90 and .25 < w/h < 1.5
                    and cv2.contourArea(contour)/(w*h) > .55):
                boxes.add((x,y,w,h))
    return sorted(boxes)


def match_board_templates(frame,folder,seat):
    area=cv2.cvtColor(frame,cv2.COLOR_BGR2GRAY)[250:470,270:1100]
    best=(0,None)
    for path in folder.glob(str(seat)+"*.png"):
        template=cv2.imread(str(path),cv2.IMREAD_GRAYSCALE)
        if template is None:continue
        for k in range(12,37):
            scale=k/20;w,h=round(template.shape[1]*scale),round(template.shape[0]*scale)
            if w<10 or h<18 or h>area.shape[0]:continue
            _,score,_,point=cv2.minMaxLoc(cv2.matchTemplate(area,cv2.resize(template,(w,h)),cv2.TM_CCOEFF_NORMED))
            if score>best[0]:best=(score,[270+point[0],250+point[1],w,h])
    return best
