import * as pulumi from "@pulumi/pulumi";
import * as random from "@pulumi/random";

const cfg = new pulumi.Config();
const length = cfg.getNumber("length") ?? 2;

const pet = new random.RandomPet("pet", { length, separator: "-" });

export const petName = pet.id;
