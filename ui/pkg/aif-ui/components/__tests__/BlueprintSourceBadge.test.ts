// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { mount } from "@vue/test-utils";
import BlueprintSourceBadge from "../BlueprintSourceBadge.vue";

describe("BlueprintSourceBadge", () => {
  it("renders text label for Custom source without icon", () => {
    const wrapper = mount(BlueprintSourceBadge, {
      props: { source: "Custom" },
    });
    expect(wrapper.text()).toContain("Custom");
    expect(wrapper.find(".partner-logo").exists()).toBe(false);
    expect(wrapper.attributes("aria-label")).toBe("Source: Custom");
  });

  it("renders Nvidia dual logos when source is Nvidia and no icon", () => {
    const wrapper = mount(BlueprintSourceBadge, {
      props: { source: "Nvidia" },
    });
    expect(wrapper.find(".nvidia-logo--light").exists()).toBe(true);
    expect(wrapper.find(".nvidia-logo--dark").exists()).toBe(true);
    expect(wrapper.attributes("aria-label")).toBe("Source: Nvidia");
  });

  it("renders SUSE dual logos when source is SUSE and no icon", () => {
    const wrapper = mount(BlueprintSourceBadge, {
      props: { source: "SUSE" },
    });
    expect(wrapper.find(".suse-logo--light").exists()).toBe(true);
    expect(wrapper.find(".suse-logo--dark").exists()).toBe(true);
    expect(wrapper.attributes("aria-label")).toBe("Source: SUSE");
  });

  it("renders partner logo inside wrapper with aria-hidden and empty alt when icon is provided", () => {
    const iconUri = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";
    const wrapper = mount(BlueprintSourceBadge, {
      props: { source: "Custom", icon: iconUri },
    });
    const img = wrapper.find(".partner-logo");
    expect(img.exists()).toBe(true);
    expect(img.attributes("src")).toBe(iconUri);
    expect(img.attributes("alt")).toBe("");
    expect(img.attributes("aria-hidden")).toBe("true");
    expect(wrapper.find(".bp-source-badge__partner-logo-wrapper").exists()).toBe(true);
    expect(wrapper.attributes("aria-label")).toBe("Source: Custom");
  });

  it("falls back to text label on image load error", async () => {
    const iconUri = "https://example.com/invalid.png";
    const wrapper = mount(BlueprintSourceBadge, {
      props: { source: "Custom", icon: iconUri },
    });
    const img = wrapper.find(".partner-logo");
    expect(img.exists()).toBe(true);

    await img.trigger("error");
    expect(wrapper.find(".partner-logo").exists()).toBe(false);
    expect(wrapper.text()).toContain("Custom");
  });

  it("falls back to source logo on image load error when source is Nvidia", async () => {
    const iconUri = "https://example.com/invalid.png";
    const wrapper = mount(BlueprintSourceBadge, {
      props: { source: "Nvidia", icon: iconUri },
    });
    expect(wrapper.find(".partner-logo").exists()).toBe(true);

    await wrapper.find(".partner-logo").trigger("error");
    expect(wrapper.find(".partner-logo").exists()).toBe(false);
    expect(wrapper.find(".nvidia-logo--light").exists()).toBe(true);
  });
});
