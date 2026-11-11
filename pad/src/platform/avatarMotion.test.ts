import { describe, expect, test } from 'bun:test'
import { screenGravity } from './avatarMotion'

const G = 9.80665

describe('screenGravity', () => {
  test('an upright phone pulls the pupils down the screen', () => {
    expect(screenGravity(0, G, 0, 1)).toEqual([0, 1]) // Android
    expect(screenGravity(0, -G, 0, -1)).toEqual([0, 1]) // iOS
  })

  test('tipping the right side down pulls them right', () => {
    // Right edge down: the x axis points down, so Android reads -g on x.
    expect(screenGravity(-G, 0, 0, 1)).toEqual([1, 0])
  })

  test('landscape turns the axes with the screen', () => {
    // Top edge to the left (angle 90): the right edge is up, so x reads +g.
    expect(screenGravity(G, 0, 90, 1)).toEqual([0, 1])
    // Top edge to the right (angle 270): the right edge is down.
    expect(screenGravity(-G, 0, 270, 1)).toEqual([0, 1])
    expect(screenGravity(0, -G, 180, 1)).toEqual([0, 1])
  })

  test('flat on a table there is no pull, and shakes are capped', () => {
    expect(screenGravity(0, 0, 0, 1)).toEqual([0, 0])
    expect(screenGravity(-100, NaN, 0, 1)).toEqual([2.5, 0])
  })
})
