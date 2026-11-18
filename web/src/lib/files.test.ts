import { expect, it } from "vitest"
import { newFiles } from "./files"

const f = (name: string, size: number, lastModified = 1) =>
  new File([new Uint8Array(size)], name, { lastModified })

it("drops files already in the list, and repeats within the batch", () => {
  const a = f("a.txt", 3)
  expect(newFiles([a], [f("a.txt", 3), f("b.txt", 3), f("b.txt", 3)]).map((x) => x.name)).toEqual([
    "b.txt",
  ])
})

it("keeps a file with the same name but different content", () => {
  expect(newFiles([f("a.txt", 3)], [f("a.txt", 4), f("a.txt", 3, 2)])).toHaveLength(2)
})
