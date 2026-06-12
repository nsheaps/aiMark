import { Ajv } from "ajv";
import addFormats from "ajv-formats";
import runV1Schema from "../schemas/run.v1.json" with { type: "json" };
import suiteManifestV1Schema from "../schemas/suite-manifest.v1.json" with { type: "json" };
import samplesV1Schema from "../schemas/samples.v1.json" with { type: "json" };

export type { AimarkRunV1 } from "./generated/run.v1";
export type { AimarkSuiteManifestV1 } from "./generated/suite-manifest.v1";
export type { AimarkSamplesV1 } from "./generated/samples.v1";

const ajv = new Ajv({ allErrors: true, strict: false });
addFormats(ajv);

export const validateRunV1 = ajv.compile(runV1Schema);
export const validateSuiteManifestV1 = ajv.compile(suiteManifestV1Schema);
export const validateSamplesV1 = ajv.compile(samplesV1Schema);

export const RUN_SCHEMA_VERSION = "aimark.run.v1";
export const SAMPLES_SCHEMA_VERSION = "aimark.samples.v1";
